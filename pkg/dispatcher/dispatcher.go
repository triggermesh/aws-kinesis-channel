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

package dispatcher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go/service/kinesis"
	eventingduck "github.com/knative/eventing/pkg/apis/duck/v1alpha1"
	eventingv1alpha1 "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	"github.com/knative/eventing/pkg/logging"
	"github.com/knative/eventing/pkg/provisioners"
	"github.com/lxc/lxd/shared/logger"
	"github.com/triggermesh/aws-kinesis-provisioner/pkg/apis/messaging/v1alpha1"
	"github.com/triggermesh/aws-kinesis-provisioner/pkg/kinesisutil"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
)

// SubscriptionsSupervisor manages the state of Kinesis Streaming subscriptions
type SubscriptionsSupervisor struct {
	logger *zap.Logger

	receiver   *provisioners.MessageReceiver
	dispatcher *provisioners.MessageDispatcher

	mux             sync.Mutex
	kinesisSessions map[provisioners.ChannelReference]stream
	subscriptions   map[provisioners.ChannelReference]map[subscriptionReference]bool

	hostToChannelMap atomic.Value
}

type stream struct {
	StreamName string
	Client     *kinesis.Kinesis
}

type headers map[string]string

type data struct {
	Headers headers `json:"headers"`
	Payload string  `json:"payload"`
}

// NewDispatcher returns a new SubscriptionsSupervisor.
func NewDispatcher(logger *zap.Logger) (*SubscriptionsSupervisor, error) {
	d := &SubscriptionsSupervisor{
		logger:          logger,
		dispatcher:      provisioners.NewMessageDispatcher(logger.Sugar()),
		kinesisSessions: make(map[provisioners.ChannelReference]stream),
		subscriptions:   make(map[provisioners.ChannelReference]map[subscriptionReference]bool),
	}
	d.setHostToChannelMap(map[string]provisioners.ChannelReference{})
	receiver, err := provisioners.NewMessageReceiver(
		createReceiverFunction(d, logger.Sugar()),
		logger.Sugar(),
		provisioners.ResolveChannelFromHostHeader(provisioners.ResolveChannelFromHostFunc(d.getChannelReferenceFromHost)))
	if err != nil {
		return nil, err
	}
	d.receiver = receiver
	return d, nil
}

// func (s *SubscriptionsSupervisor) signalReconnect() {
// 	select {
// 	case s.connect <- struct{}{}:
// 		// Sent.
// 	default:
// 		// The Channel is already full, so a reconnection attempt will occur.
// 	}
// }

func createReceiverFunction(s *SubscriptionsSupervisor, logger *zap.SugaredLogger) func(provisioners.ChannelReference, *provisioners.Message) error {
	return func(channel provisioners.ChannelReference, m *provisioners.Message) error {
		logger.Infof("Received message from %q channel", channel.String())
		// publish to kinesis
		message, err := json.Marshal(m)
		if err != nil {
			logger.Errorf("Error during marshaling of the message: %v", err)
			return err
		}
		cRef := provisioners.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
		kk, present := s.kinesisSessions[cRef]
		if !present {
			logger.Errorf("Kinesis session not initialized")
			return err
		}
		if err := kinesisutil.Publish(kk.Client, kk.StreamName, message, logger); err != nil {
			logger.Errorf("Error during publish: %v", err)
			return err
		}
		logger.Infof("Published [%s] : '%s'", channel.String(), m.Headers)
		return nil
	}
}

func (s *SubscriptionsSupervisor) Start(stopCh <-chan struct{}) error {
	// Starting Connect to establish connection with Kinesis
	// go s.Connect(stopCh)
	// Trigger Connect to establish connection with Kinesis
	// s.signalReconnect()
	return s.receiver.Start(stopCh)
}

// func (s *SubscriptionsSupervisor) connectWithRetry(stopCh <-chan struct{}) {
// 	// re-attempting evey 1 second until the connection is established.
// 	ticker := time.NewTicker(retryInterval)
// 	defer ticker.Stop()
// 	// for {
// 	// 	kConn, err := kinesisutil.Connect(s.accountAccessKeyID, s.accountSecretAccessKey, s.region, s.logger.Sugar())
// 	// 	if err == nil {
// 	// 		// Locking here in order to reduce time in locked state.
// 	// 		s.kinesisConnMux.Lock()
// 	// 		s.kinesisConn = kConn
// 	// 		s.kinesisConnInProgress = false
// 	// 		s.kinesisConnMux.Unlock()
// 	// 		return
// 	// 	}
// 	// 	s.logger.Sugar().Errorf("Connect() failed with error: %+v, retrying in %s", err, retryInterval.String())
// 	// 	select {
// 	// 	case <-ticker.C:
// 	// 		continue
// 	// 	case <-stopCh:
// 	// 		return
// 	// 	}
// 	// }
// }

// Connect is called for initial connection as well as after every disconnect
// func (s *SubscriptionsSupervisor) Connect(stopCh <-chan struct{}) {
// 	for {
// 		select {
// 		case <-s.connect:
// 			s.kinesisConnMux.Lock()
// 			currentConnProgress := s.kinesisConnInProgress
// 			s.kinesisConnMux.Unlock()
// 			if !currentConnProgress {
// 				// Case for lost connectivity, setting InProgress to true to prevent recursion
// 				s.kinesisConnMux.Lock()
// 				s.kinesisConnInProgress = true
// 				s.kinesisConnMux.Unlock()
// 				go s.connectWithRetry(stopCh)
// 			}
// 		case <-stopCh:
// 			return
// 		}
// 	}
// }

// UpdateSubscriptions creates/deletes the kinesis subscriptions based on channel.Spec.Subscribable.Subscribers
// Return type:map[eventingduck.SubscriberSpec]error --> Returns a map of subscriberSpec that failed with the value=error encountered.
// Ignore the value in case error != nil
func (s *SubscriptionsSupervisor) UpdateSubscriptions(channel *v1alpha1.KinesisChannel, isFinalizer bool) (map[eventingduck.SubscriberSpec]error, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	failedToSubscribe := make(map[eventingduck.SubscriberSpec]error)
	cRef := provisioners.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	if channel.Spec.Subscribable == nil || isFinalizer {
		s.logger.Sugar().Infof("Empty subscriptions for channel Ref: %v; unsubscribe all active subscriptions, if any", cRef)
		chMap, ok := s.subscriptions[cRef]
		if !ok {
			// nothing to do
			s.logger.Sugar().Infof("No channel Ref %v found in subscriptions map", cRef)
			return failedToSubscribe, nil
		}
		for sub := range chMap {
			s.unsubscribe(cRef, sub)
		}
		delete(s.subscriptions, cRef)
		return failedToSubscribe, nil
	}

	subscriptions := channel.Spec.Subscribable.Subscribers
	activeSubs := make(map[subscriptionReference]bool) // it's logically a set

	chMap, ok := s.subscriptions[cRef]
	if !ok {
		chMap = make(map[subscriptionReference]bool)
		s.subscriptions[cRef] = chMap
	}
	var errStrings []string
	for _, sub := range subscriptions {
		// check if the subscription already exist and do nothing in this case
		subRef := newSubscriptionReference(sub)
		if _, ok := chMap[subRef]; ok {
			activeSubs[subRef] = true
			s.logger.Sugar().Infof("Subscription: %v already active for channel: %v", sub, cRef)
			continue
		}
		// subscribe and update failedSubscription if subscribe fails
		err := s.subscribe(cRef, subRef)
		if err != nil {
			errStrings = append(errStrings, err.Error())
			s.logger.Sugar().Errorf("failed to subscribe (subscription:%q) to channel: %v. Error:%s", sub, cRef, err.Error())
			failedToSubscribe[sub] = err
			continue
		}
		chMap[subRef] = true
		activeSubs[subRef] = true
	}
	// Unsubscribe for deleted subscriptions
	for sub := range chMap {
		if ok := activeSubs[sub]; !ok {
			s.unsubscribe(cRef, sub)
		}
	}
	// delete the channel from s.subscriptions if chMap is empty
	if len(s.subscriptions[cRef]) == 0 {
		delete(s.subscriptions, cRef)
	}
	return failedToSubscribe, nil
}

func (s *SubscriptionsSupervisor) subscribe(channel provisioners.ChannelReference, subscription subscriptionReference) error {
	s.logger.Info("Subscribe to channel:", zap.Any("channel", channel), zap.Any("subscription", subscription))

	session, present := s.kinesisSessions[channel]
	if !present {
		s.logger.Error("Kinesis session not found:", zap.Any("channel", channel))
		return fmt.Errorf("Kinesis session for channel %q not found", channel.String())
	}
	iterator, err := kinesisutil.GetShardIterator(session.Client, &session.StreamName)
	if err != nil {
		s.logger.Error("Kinesis shard iterator request error:", zap.Error(err))
		return fmt.Errorf("Kinesis shard iterator request error: %s", err)
	}
	go func(nextRecord *string, channel provisioners.ChannelReference, subscription subscriptionReference) {
		var message data
		for {
			if _, exist := s.subscriptions[channel][subscription]; !exist {
				s.logger.Info("Subscription not found, exiting")
				return
			}
			if nextRecord == nil {
				s.logger.Info("Null shard iterator, stop subscriber process. Is the stream closed?")
				return
			}
			record, err := kinesisutil.GetRecord(session.Client, nextRecord)
			if err != nil {
				s.logger.Error("Error reading Kinesis stream message:", zap.Error(err))
				continue
			}
			nextRecord = record.NextShardIterator
			if len(record.Records) == 0 {
				continue
			}
			if err := json.Unmarshal(record.Records[0].Data, &message); err != nil {
				s.logger.Error("Error decoding message:", zap.Error(err))
				continue
			}
			payload, err := base64.StdEncoding.DecodeString(message.Payload)
			if err != nil {
				s.logger.Error("Error decoding payload:", zap.Error(err))
				continue
			}
			if err := s.dispatcher.DispatchMessage(&provisioners.Message{
				Headers: message.Headers,
				Payload: payload,
			}, subscription.SubscriberURI, subscription.ReplyURI, provisioners.DispatchDefaults{
				Namespace: channel.Namespace,
			}); err != nil {
				s.logger.Error("Message dispatching error:", zap.Error(err))
			}
		}
	}(iterator.ShardIterator, channel, subscription)
	return nil
}

// should be called only while holding subscriptionsMux
func (s *SubscriptionsSupervisor) unsubscribe(channel provisioners.ChannelReference, subscription subscriptionReference) error {
	s.logger.Info("Unsubscribe from channel:", zap.Any("channel", channel), zap.Any("subscription", subscription))

	if _, ok := s.subscriptions[channel][subscription]; ok {
		delete(s.subscriptions[channel], subscription)
	}
	return nil
}

func (s *SubscriptionsSupervisor) getHostToChannelMap() map[string]provisioners.ChannelReference {
	return s.hostToChannelMap.Load().(map[string]provisioners.ChannelReference)
}

func (s *SubscriptionsSupervisor) setHostToChannelMap(hcMap map[string]provisioners.ChannelReference) {
	s.hostToChannelMap.Store(hcMap)
}

// UpdateHostToChannelMap will be called from the controller that watches kinesis channels.
// It will update internal hostToChannelMap which is used to resolve the hostHeader of the
// incoming request to the correct ChannelReference in the receiver function.
func (s *SubscriptionsSupervisor) UpdateHostToChannelMap(ctx context.Context, chanList []eventingv1alpha1.Channel) error {
	hostToChanMap, err := provisioners.NewHostNameToChannelRefMap(chanList)
	if err != nil {
		logging.FromContext(ctx).Info("UpdateHostToChannelMap: Error occurred when creating the new hostToChannel map.", zap.Error(err))
		return err
	}
	s.setHostToChannelMap(hostToChanMap)
	logging.FromContext(ctx).Info("hostToChannelMap updated successfully.")
	return nil
}

func (s *SubscriptionsSupervisor) getChannelReferenceFromHost(host string) (provisioners.ChannelReference, error) {
	chMap := s.getHostToChannelMap()
	cr, ok := chMap[host]
	if !ok {
		return cr, fmt.Errorf("Invalid HostName:%q. HostName not found in any of the watched kinesis channels", host)
	}
	return cr, nil
}

func (s *SubscriptionsSupervisor) KinesisSessionExist(ctx context.Context, channel *v1alpha1.KinesisChannel) bool {
	s.mux.Lock()
	defer s.mux.Unlock()
	cRef := provisioners.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	_, present := s.kinesisSessions[cRef]
	return present
}

func (s *SubscriptionsSupervisor) CreateKinesisSession(ctx context.Context, channel *v1alpha1.KinesisChannel, secret *corev1.Secret) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	cRef := provisioners.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	_, present := s.kinesisSessions[cRef]
	if !present {
		client, err := s.kinesisClient(channel.Spec.StreamName, channel.Spec.AccountRegion, secret)
		if err != nil {
			logger.Errorf("Error creating Kinesis session: %v", err)
			return err
		}
		s.kinesisSessions[cRef] = stream{
			StreamName: channel.Spec.StreamName,
			Client:     client,
		}
	}
	return nil
}

func (s *SubscriptionsSupervisor) kinesisClient(stream, region string, creds *corev1.Secret) (*kinesis.Kinesis, error) {
	if creds == nil {
		return nil, fmt.Errorf("Credentials data is nil")
	}
	keyID, present := creds.Data["aws_access_key_id"]
	if !present {
		return nil, fmt.Errorf("\"aws_access_key_id\" secret key is missing")
	}
	secret, present := creds.Data["aws_secret_access_key"]
	if !present {
		return nil, fmt.Errorf("\"aws_secret_access_key\" secret key is missing")
	}
	return kinesisutil.Connect(string(keyID), string(secret), region, s.logger.Sugar())
}
