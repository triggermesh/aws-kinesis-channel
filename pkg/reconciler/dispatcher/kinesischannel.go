/*
Copyright (c) 2018 TriggerMesh, Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes/scheme"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	eventingduckv1beta1 "knative.dev/eventing/pkg/apis/duck/v1beta1"
	"knative.dev/eventing/pkg/channel/fanout"
	"knative.dev/eventing/pkg/channel/multichannelfanout"
	"knative.dev/eventing/pkg/logging"
	"knative.dev/pkg/client/injection/kube/informers/core/v1/secret"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"

	"github.com/triggermesh/aws-kinesis-channel/pkg/apis/messaging/v1alpha1"
	kinesisclientset "github.com/triggermesh/aws-kinesis-channel/pkg/client/clientset/internalclientset"
	kinesisscheme "github.com/triggermesh/aws-kinesis-channel/pkg/client/clientset/internalclientset/scheme"
	kinesisclient "github.com/triggermesh/aws-kinesis-channel/pkg/client/injection/client"
	"github.com/triggermesh/aws-kinesis-channel/pkg/client/injection/informers/messaging/v1alpha1/kinesischannel"
	listers "github.com/triggermesh/aws-kinesis-channel/pkg/client/listers/messaging/v1alpha1"
	"github.com/triggermesh/aws-kinesis-channel/pkg/dispatcher"
)

const (
	// ReconcilerName is the name of the reconciler.
	ReconcilerName = "KinesisChannels"

	// controllerAgentName is the string used by this controller to identify
	// itself when creating events.
	controllerAgentName = "kinesis-ch-dispatcher"

	finalizerName = controllerAgentName

	channelReconcileFailed    = "ChannelReconcileFailed"
	channelUpdateStatusFailed = "ChannelUpdateStatusFailed"
)

// Reconciler reconciles Kinesis Channels.
type Reconciler struct {
	recorder record.EventRecorder

	kinesisDispatcher *dispatcher.KinesisDispatcher

	kinesisClientSet     kinesisclientset.Interface
	kinesischannelLister listers.KinesisChannelLister
	secretLister         corev1listers.SecretLister
	impl                 *controller.Impl
}

// Check that our Reconciler implements controller.Reconciler.
var _ controller.Reconciler = (*Reconciler)(nil)

func init() {
	// Add run types to the default Kubernetes Scheme so Events can be
	// logged for run types.
	utilruntime.Must(kinesisscheme.AddToScheme(scheme.Scheme))
}

// NewController initializes the controller and is called by the generated code.
// Registers event handlers to enqueue events.
func NewController(ctx context.Context, _ configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)

	kinesisDispatcher, err := dispatcher.NewDispatcher(ctx)
	if err != nil {
		logger.Fatal("Unable to create kinesis dispatcher", zap.Error(err))
	}

	kinesisChannelInformer := kinesischannel.Get(ctx)
	secretsInformer := secret.Get(ctx)

	r := &Reconciler{
		recorder: controller.GetEventRecorder(ctx),

		kinesisDispatcher: kinesisDispatcher,

		kinesisClientSet:     kinesisclient.Get(ctx),
		kinesischannelLister: kinesisChannelInformer.Lister(),
		secretLister:         secretsInformer.Lister(),
	}
	r.impl = controller.NewImpl(r, logger.Sugar(), ReconcilerName)

	logger.Info("Setting up event handlers")

	// Watch for kinesis channels.
	kinesisChannelInformer.Informer().AddEventHandler(controller.HandleAll(r.impl.Enqueue))

	logger.Info("Starting dispatcher")
	go func() {
		if err := kinesisDispatcher.Start(ctx); err != nil {
			logger.Error("Cannot start dispatcher", zap.Error(err))
		}
	}()

	return r.impl
}

func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name.
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		logging.FromContext(ctx).Error("invalid resource key")
		return nil
	}

	// Get the KinesisChannel resource with this namespace/name.
	original, err := r.kinesischannelLister.KinesisChannels(namespace).Get(name)
	if apierrs.IsNotFound(err) {
		// The resource may no longer exist, in which case we stop processing.
		logging.FromContext(ctx).Error("KinesisChannel key in work queue no longer exists")
		return nil
	} else if err != nil {
		return err
	}

	if !original.Status.IsReady() {
		return fmt.Errorf("channel is not ready. Cannot configure and update subscriber status")
	}

	// Don't modify the informers copy.
	kinesisChannel := original.DeepCopy()

	reconcileErr := r.reconcile(ctx, kinesisChannel)
	if reconcileErr != nil {
		logging.FromContext(ctx).Error("Error reconciling KinesisChannel", zap.Error(reconcileErr))
		r.recorder.Eventf(kinesisChannel, corev1.EventTypeWarning, channelReconcileFailed, "KinesisChannel reconciliation failed: %v", reconcileErr)
	} else {
		logging.FromContext(ctx).Debug("KinesisChannel reconciled")
	}

	// TODO: Should this check for subscribable status rather than entire status?
	if _, updateStatusErr := r.updateStatus(kinesisChannel); updateStatusErr != nil {
		logging.FromContext(ctx).Error("Failed to update KinesisChannel status", zap.Error(updateStatusErr))
		r.recorder.Eventf(kinesisChannel, corev1.EventTypeWarning, channelUpdateStatusFailed, "Failed to update KinesisChannel's status: %v", updateStatusErr)
		return updateStatusErr
	}

	return reconcileErr
}

func (r *Reconciler) reconcile(ctx context.Context, kinesisChannel *v1alpha1.KinesisChannel) error {
	// TODO update dispatcher API and use Channelable or KinesisChannel.

	// See if the channel has been deleted.
	if kinesisChannel.DeletionTimestamp != nil {
		if _, err := r.kinesisDispatcher.UpdateSubscriptions(ctx, kinesisChannel, true); err != nil {
			logging.FromContext(ctx).Error("Error updating subscriptions", zap.Any("channel", kinesisChannel), zap.Error(err))
			return err
		}
		r.kinesisDispatcher.DeleteKinesisSession(ctx, kinesisChannel)
		removeFinalizer(kinesisChannel)
		_, err := r.kinesisClientSet.MessagingV1alpha1().KinesisChannels(kinesisChannel.Namespace).Update(kinesisChannel)
		return err
	}

	// If we are adding the finalizer for the first time, then ensure that finalizer is persisted
	// before manipulating Kinesis.
	if err := r.ensureFinalizer(kinesisChannel); err != nil {
		return err
	}

	if !r.kinesisDispatcher.KinesisSessionExist(ctx, kinesisChannel) {
		secret, err := r.secretLister.Secrets(kinesisChannel.Namespace).Get(kinesisChannel.Spec.AccountCreds)
		if err != nil {
			return err
		}
		if err := r.kinesisDispatcher.CreateKinesisSession(ctx, kinesisChannel, secret); err != nil {
			return err
		}
	}

	// Try to subscribe.
	failedSubscriptions, err := r.kinesisDispatcher.UpdateSubscriptions(ctx, kinesisChannel, false)
	if err != nil {
		logging.FromContext(ctx).Error("Error updating subscriptions", zap.Any("channel", kinesisChannel), zap.Error(err))
		return err
	}

	kinesisChannel.Status.SubscribableStatus = r.createSubscribableStatus(kinesisChannel.Spec.SubscribableSpec, failedSubscriptions)
	if len(failedSubscriptions) > 0 {
		logging.FromContext(ctx).Error("Some kinesis subscriptions failed to subscribe")
		return fmt.Errorf("some kinesis subscriptions failed to subscribe")
	}

	kinesisChannels, err := r.kinesischannelLister.List(labels.Everything())
	if err != nil {
		logging.FromContext(ctx).Error("Error listing kinesis channels")
		return err
	}

	channels := make([]*v1alpha1.KinesisChannel, 0)
	for _, c := range kinesisChannels {
		if c.Status.IsReady() {
			channels = append(channels, c)
		}
	}

	config := r.newConfigFromKinesisChannels(channels)

	if err := r.kinesisDispatcher.UpdateHostToChannelMap(config); err != nil {
		logging.FromContext(ctx).Error("Error updating host to channel map", zap.Error(err))
		return err
	}

	return nil
}

func (r *Reconciler) createSubscribableStatus(subscribable eventingduckv1beta1.SubscribableSpec, failedSubscriptions map[eventingduckv1beta1.SubscriberSpec]error) eventingduckv1beta1.SubscribableStatus {
	if len(subscribable.Subscribers) == 0 {
		return eventingduckv1beta1.SubscribableStatus{}
	}
	subscriberStatus := make([]eventingduckv1beta1.SubscriberStatus, 0)
	for _, sub := range subscribable.Subscribers {
		status := eventingduckv1beta1.SubscriberStatus{
			UID:                sub.UID,
			ObservedGeneration: sub.Generation,
			Ready:              corev1.ConditionTrue,
		}
		if err, ok := failedSubscriptions[sub]; ok {
			status.Ready = corev1.ConditionFalse
			status.Message = err.Error()
		}
		subscriberStatus = append(subscriberStatus, status)
	}
	return eventingduckv1beta1.SubscribableStatus{
		Subscribers: subscriberStatus,
	}
}

func (r *Reconciler) updateStatus(desired *v1alpha1.KinesisChannel) (*v1alpha1.KinesisChannel, error) {
	nc, err := r.kinesischannelLister.KinesisChannels(desired.Namespace).Get(desired.Name)
	if err != nil {
		return nil, err
	}

	if reflect.DeepEqual(nc.Status, desired.Status) {
		return nc, nil
	}

	// Don't modify the informers copy.
	existing := nc.DeepCopy()
	existing.Status = desired.Status

	return r.kinesisClientSet.MessagingV1alpha1().KinesisChannels(desired.Namespace).UpdateStatus(existing)
}

func (r *Reconciler) ensureFinalizer(channel *v1alpha1.KinesisChannel) error {
	finalizers := sets.NewString(channel.Finalizers...)
	if finalizers.Has(finalizerName) {
		return nil
	}

	mergePatch := map[string]interface{}{
		"metadata": map[string]interface{}{
			"finalizers":      append(channel.Finalizers, finalizerName),
			"resourceVersion": channel.ResourceVersion,
		},
	}

	patch, err := json.Marshal(mergePatch)
	if err != nil {
		return err
	}

	_, err = r.kinesisClientSet.MessagingV1alpha1().KinesisChannels(channel.GetNamespace()).Patch(channel.GetName(), types.MergePatchType, patch)
	return err
}

func removeFinalizer(channel *v1alpha1.KinesisChannel) {
	finalizers := sets.NewString(channel.Finalizers...)
	finalizers.Delete(finalizerName)
	channel.Finalizers = finalizers.List()
}

// newConfigFromKinesisChannels creates a new Config from the list of kinesis channels.
func (r *Reconciler) newConfigFromKinesisChannels(channels []*v1alpha1.KinesisChannel) *multichannelfanout.Config {
	cc := make([]multichannelfanout.ChannelConfig, 0)
	for _, c := range channels {
		channelConfig := r.newChannelConfigFromKinesisChannel(c)
		cc = append(cc, *channelConfig)
	}
	return &multichannelfanout.Config{
		ChannelConfigs: cc,
	}
}

// newConfigFromKinesisChannels creates a new Config from the list of kinesis channels.
func (r *Reconciler) newChannelConfigFromKinesisChannel(c *v1alpha1.KinesisChannel) *multichannelfanout.ChannelConfig {
	channelConfig := multichannelfanout.ChannelConfig{
		Namespace: c.Namespace,
		Name:      c.Name,
		HostName:  c.Status.Address.Hostname,
	}
	if c.Spec.Subscribers != nil {
		channelConfig.FanoutConfig = fanout.Config{
			AsyncHandler:  true,
			Subscriptions: c.Spec.Subscribers,
		}
	}
	return &channelConfig
}
