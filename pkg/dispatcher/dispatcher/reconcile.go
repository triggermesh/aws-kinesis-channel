// Copyright 2019 TriggerMesh, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package dispatcher

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/kinesis"
	eventingduck "github.com/knative/eventing/pkg/apis/duck/v1alpha1"
	eventingv1alpha1 "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	"github.com/knative/eventing/pkg/logging"
	"github.com/knative/eventing/pkg/provisioners"
	ccpcontroller "github.com/triggermesh/aws-kinesis-provisioner/pkg/controller/clusterchannelprovisioner"
	provisioner "github.com/triggermesh/aws-kinesis-provisioner/pkg/kinesis"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	finalizerName  = controllerAgentName
	backoffTimeout = 5 * time.Second

	dispatcherReconciled         = "DispatcherReconciled"
	dispatcherReconcileFailed    = "DispatcherReconcileFailed"
	dispatcherUpdateStatusFailed = "DispatcherUpdateStatusFailed"
)

type channelName = types.NamespacedName
type subscriberKey = string

type reconciler struct {
	client   client.Client
	recorder record.EventRecorder
	logger   *zap.Logger

	reconcileChan chan<- event.GenericEvent
	dispatcher    provisioners.Dispatcher
	channelLock   sync.RWMutex
	channels      map[channelName]*receiver
	kClient       *provisioner.Client
}

type receiver struct {
	subscriberLock sync.RWMutex
	subscribers    map[subscriberKey]*subscriber
	stopReceiver   context.CancelFunc
}

type subscriber struct {
	messages       chan *provisioners.Message
	stopSubscriber context.CancelFunc
}

var _ reconcile.Reconciler = &reconciler{}

func (r *reconciler) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

func (r *reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := logging.WithLogger(context.TODO(), r.logger.With(zap.Any("request", request)))

	c := &eventingv1alpha1.Channel{}
	err := r.client.Get(ctx, request.NamespacedName, c)

	if k8serrors.IsNotFound(err) {
		logging.FromContext(ctx).Info("Could not find Channel", zap.Error(err))
		return reconcile.Result{}, nil
	}

	// Any other error should be retried in another reconciliation.
	if err != nil {
		logging.FromContext(ctx).Error("Unable to Get Channel", zap.Error(err))
		return reconcile.Result{}, err
	}

	// Does this Controller control this Channel?
	if !r.shouldReconcile(c) {
		logging.FromContext(ctx).Info("Not reconciling Channel, it is not controlled by this Controller", zap.Any("ref", c.Spec))
		return reconcile.Result{}, nil
	}

	logging.FromContext(ctx).Info("Reconciling Channel")
	c = c.DeepCopy()

	requeue, reconcileErr := r.reconcile(logging.With(ctx, zap.Any("channel", c)), c)
	if reconcileErr != nil {
		logging.FromContext(ctx).Info("Error reconciling Channel", zap.Error(reconcileErr))
		r.recorder.Eventf(c, v1.EventTypeWarning, dispatcherReconcileFailed, "Dispatcher reconciliation failed: %v", err)
		// Note that we do not return the error here, because we want to update the finalizers
		// regardless of the error.
	} else {
		logging.FromContext(ctx).Info("Channel reconciled")
		r.recorder.Eventf(c, v1.EventTypeNormal, dispatcherReconciled, "Dispatcher reconciled: %q", c.Name)
	}

	if err = provisioners.UpdateChannel(ctx, r.client, c); err != nil {
		logging.FromContext(ctx).Info("Error updating Channel Status", zap.Error(err))
		r.recorder.Eventf(c, v1.EventTypeWarning, dispatcherUpdateStatusFailed, "Failed to update Channel's dispatcher status: %v", err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{
		Requeue: requeue,
	}, reconcileErr
}

func (r *reconciler) reconcile(ctx context.Context, c *eventingv1alpha1.Channel) (bool, error) {
	channelKey := key(c)

	if c.DeletionTimestamp != nil {
		r.stopReceiver(ctx, channelKey)
		provisioners.RemoveFinalizer(c, finalizerName)
		return false, nil
	}

	if addFinalizerResult := provisioners.AddFinalizer(c, finalizerName); addFinalizerResult == provisioners.FinalizerAdded {
		return true, nil
	}

	r.syncSubscribers(ctx, channelKey, c.Spec.Subscribable)
	return false, nil
}

func (r *reconciler) shouldReconcile(c *eventingv1alpha1.Channel) bool {
	if c.Spec.Provisioner != nil {
		return ccpcontroller.IsControlled(c.Spec.Provisioner)
	}
	return false
}

// key creates the first index into reconciler.subscriptions, based on the Channel's name.
func key(c *eventingv1alpha1.Channel) channelName {
	return types.NamespacedName{
		Namespace: c.Namespace,
		Name:      c.Name,
	}
}

func (r *reconciler) channelSubscribers(channelKey channelName) (map[subscriberKey]*subscriber, bool) {
	r.channelLock.RLock()
	defer r.channelLock.RUnlock()

	if r.channels == nil {
		return nil, true
	}
	channel, present := r.channels[channelKey]
	if !present {
		return nil, true
	}

	channel.subscriberLock.RLock()
	defer channel.subscriberLock.RUnlock()

	if channel.subscribers == nil {
		return nil, false
	}
	return channel.subscribers, len(channel.subscribers) == 0
}

func (r *reconciler) stopReceiver(ctx context.Context, channelKey channelName) {
	r.channelLock.RLock()
	defer r.channelLock.RUnlock()

	if channel, present := r.channels[channelKey]; present {
		channel.subscriberLock.RLock()
		defer channel.subscriberLock.RUnlock()
		for name := range channel.subscribers {
			// Stopping message forwarding process
			channel.subscribers[name].stopSubscriber()
		}
		// Stopping channel message receiver
		channel.stopReceiver()
		// delete(r.channels, channelKey)
	}
}

func (r *reconciler) syncSubscribers(ctx context.Context, channelKey channelName, subscribers *eventingduck.Subscribable) {
	r.channelLock.Lock()
	defer r.channelLock.Unlock()

	if _, present := r.channels[channelKey]; !present {
		// channel doesn't have message receiver yet
		ctxWithCancel, cancelFunc := context.WithCancel(ctx)
		r.channels[channelKey] = &receiver{
			subscribers:    make(map[subscriberKey]*subscriber),
			subscriberLock: sync.RWMutex{},
			stopReceiver:   cancelFunc,
		}
		go func() {
			// fmt.Printf("starting receiver %q\n", channelKey)
			r.messageReader(ctxWithCancel, channelKey)

			r.channelLock.Lock()
			defer r.channelLock.Unlock()
			// fmt.Printf("receiver %q exited, cleaning up\n", channelKey)
			delete(r.channels, channelKey)
		}()
	}
	r.createSubscribers(ctx, channelKey, r.channels[channelKey], subscribers)
	r.cleanupSubscribers(r.channels[channelKey], subscribers)
}

func (r *reconciler) cleanupSubscribers(channel *receiver, subscribers *eventingduck.Subscribable) {
	for activeSub := range channel.subscribers {
		var exists bool
		if subscribers != nil {
			for _, listedSub := range subscribers.Subscribers {
				if listedSub.Ref.Name == activeSub {
					exists = true
					break
				}
			}
		}
		if !exists {
			// Removing orphaned subscription process
			// fmt.Printf("Stopping orphaned subscriber %q\n", activeSub)
			channel.subscribers[activeSub].stopSubscriber()
		}
	}
}

func (r *reconciler) createSubscribers(ctx context.Context, channelKey channelName, channel *receiver, subscribers *eventingduck.Subscribable) {
	channel.subscriberLock.Lock()
	defer channel.subscriberLock.Unlock()

	if subscribers == nil {
		return
	}
	for _, channelSub := range subscribers.Subscribers {
		if channelSub.Ref == nil {
			continue
		}

		if _, present := channel.subscribers[channelSub.Ref.Name]; !present {
			ctxWithCancel, cancelFunc := context.WithCancel(ctx)
			channel.subscribers[channelSub.Ref.Name] = &subscriber{
				messages:       make(chan *provisioners.Message),
				stopSubscriber: cancelFunc,
			}
			// Starting message forwarding background process
			go func() {
				// fmt.Printf("starting subscriber %q\n", channelSub.Ref.Name)
				r.messageForwarder(ctxWithCancel, channelKey, channelSub)
				// Cleanup if process exited
				// fmt.Printf("subscriber %q exited, cleaning up\n", channelSub.Ref.Name)

				channel.subscriberLock.Lock()
				defer channel.subscriberLock.Unlock()
				close(channel.subscribers[channelSub.Ref.Name].messages)
				delete(channel.subscribers, channelSub.Ref.Name)
			}()
		}
	}
}

// messageReader destructively reads a message from the Kinesis stream
// and tries to send it to a forwarder channel
// If the message was accepted by at least one forwarder channel
// we are considering it as successfully delivered
func (r *reconciler) messageReader(ctx context.Context, channelKey channelName) {
	var nextShardIterator *string

	streamDescription, err := r.kClient.DescribeKinesisStream(channelKey.Name)
	if err != nil {
		r.logger.Error(fmt.Sprint("Could not Get Stream. Error: ", err))
		return
	}

	// Obtain starting Shard Iterator. This is needed to not process already processed records
	shardIterator, err := r.kClient.Kinesis.GetShardIterator(&kinesis.GetShardIteratorInput{
		ShardId:           streamDescription.Shards[0].ShardId,
		ShardIteratorType: aws.String("LATEST"),
		StreamName:        streamDescription.StreamName,
	})
	nextShardIterator = shardIterator.ShardIterator

	if err != nil {
		r.logger.Error(fmt.Sprint("Could not Get ShardIterator. Error: ", err))
		return
	}

	for {
		select {
		case <-ctx.Done():
			// Exiting process
			return
		default:
			subscribers, empty := r.channelSubscribers(channelKey)
			if empty {
				continue
			}

			message, newShardIterator, err := r.kClient.GetStreamMessage(nextShardIterator)
			if err != nil {
				r.logger.Error(fmt.Sprint("Could not Get StreamMessage. Error: ", err))
				continue
			}
			nextShardIterator = newShardIterator

			r.channels[channelKey].subscriberLock.RLock()
			for _, subscriber := range subscribers {
				subscriber.messages <- &message
			}
			r.channels[channelKey].subscriberLock.RUnlock()
		}
	}
}

func (r *reconciler) messageForwarder(ctx context.Context, channelKey channelName, subscriber eventingduck.ChannelSubscriberSpec) {
	subscribers, empty := r.channelSubscribers(channelKey)
	if empty {
		r.logger.Warn("Channel doesn't have subscribers")
		return
	}
	if subscriber.Ref == nil {
		r.logger.Warn("Can't get a subscriber reference")
		return
	}
	incomingChan, present := subscribers[subscriber.Ref.Name]
	if !present {
		r.logger.Warn("Channel subscriber doesn't exist")
		return
	}
	defaults := provisioners.DispatchDefaults{
		Namespace: channelKey.Namespace,
	}
	for {
		select {
		case <-ctx.Done():
			// Exiting process
			return
		case message := <-incomingChan.messages:
			if len(message.Payload) == 0 {
				// Do not forward empty messages to the service
				continue
			}
			err := r.dispatcher.DispatchMessage(message, subscriber.SubscriberURI, subscriber.ReplyURI, defaults)
			if err != nil {
				// TODO: Add message dispatching feedback. Without it delivery is not reliable
				r.logger.Error("Can't send the message to a subscriber", zap.Error(err))
				time.Sleep(backoffTimeout)
				continue
			}
		}
	}
}
