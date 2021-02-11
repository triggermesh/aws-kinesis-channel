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
	"errors"
	"fmt"
	nethttp "net/http"
	"sync"
	"sync/atomic"

	"github.com/aws/aws-sdk-go/service/kinesis"
	"github.com/cloudevents/sdk-go/v2/binding"
	"github.com/cloudevents/sdk-go/v2/event"

	"github.com/triggermesh/aws-kinesis-channel/pkg/apis/messaging/v1alpha1"
	"github.com/triggermesh/aws-kinesis-channel/pkg/kinesisutil"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"

	eventingduckv1 "knative.dev/eventing/pkg/apis/duck/v1"
	"knative.dev/eventing/pkg/channel"
	eventingchannels "knative.dev/eventing/pkg/channel"
	"knative.dev/eventing/pkg/channel/multichannelfanout"
	"knative.dev/pkg/logging"
)

// KinesisDispatcher manages the state of Kinesis Streaming subscriptions.
type KinesisDispatcher struct {
	logger *zap.Logger

	receiver   *eventingchannels.MessageReceiver
	dispatcher *eventingchannels.MessageDispatcherImpl

	mux             sync.Mutex
	kinesisSessions map[eventingchannels.ChannelReference]stream
	subscriptions   map[eventingchannels.ChannelReference]map[string]eventingduckv1.SubscriberSpec

	hostToChannelMap atomic.Value

	hostToChannelMapLock sync.Mutex
}

type stream struct {
	StreamName string
	Client     *kinesis.Kinesis
}

// NewDispatcher returns a new KinesisDispatcher.
func NewDispatcher(ctx context.Context) (*KinesisDispatcher, error) {
	logger := logging.FromContext(ctx).Desugar()

	d := &KinesisDispatcher{
		logger:          logger,
		dispatcher:      eventingchannels.NewMessageDispatcher(logger),
		kinesisSessions: make(map[eventingchannels.ChannelReference]stream),
		subscriptions:   make(map[eventingchannels.ChannelReference]map[string]eventingduckv1.SubscriberSpec),
	}
	d.setHostToChannelMap(map[string]eventingchannels.ChannelReference{})
	receiver, err := eventingchannels.NewMessageReceiver(
		func(ctx context.Context, channel eventingchannels.ChannelReference, m binding.Message, transformers []binding.Transformer, _ nethttp.Header) error {
			logger.Sugar().Infof("Received message from %q channel", channel.String())
			// publish to kinesis
			event, err := binding.ToEvent(ctx, m)
			if err != nil {
				logger.Sugar().Errorf("Can't convert message to event: %v", err)
				return err
			}
			eventPayload, err := event.MarshalJSON()
			if err != nil {
				logger.Sugar().Errorf("Can't encode event: %v", err)
				return err
			}
			kc, present := d.kinesisSessions[channel]
			if !present {
				logger.Sugar().Error("Message receiver: kinesis session not initialized")
				return err
			}
			if err := kinesisutil.Publish(ctx, kc.Client, kc.StreamName, eventPayload, logger.Sugar()); err != nil {
				logger.Sugar().Errorf("Error during publish: %v", err)
				return err
			}
			logger.Sugar().Infof("Published to %q kinesis stream", channel.String())
			return nil
		},
		logger,
		eventingchannels.NewStatsReporter(channel.ContainerTagKey.Name(), channel.UniqueTagKey.Name()),
		eventingchannels.ResolveMessageChannelFromHostHeader(d.getChannelReferenceFromHost))
	if err != nil {
		return nil, err
	}
	d.receiver = receiver
	return d, nil
}

func (s *KinesisDispatcher) Start(ctx context.Context) error {
	if s.receiver == nil {
		return fmt.Errorf("message receiver is not set")
	}

	return s.receiver.Start(ctx)
}

// UpdateSubscriptions creates/deletes the kinesis subscriptions based on channel.Spec.Subscribable.Subscribers.
func (s *KinesisDispatcher) UpdateSubscriptions(ctx context.Context, channel *v1alpha1.KinesisChannel, isFinalizer bool) (map[eventingduckv1.SubscriberSpec]error, error) {
	s.mux.Lock()
	defer s.mux.Unlock()

	subscriptions := channel.Spec.Subscribers
	failedSubs := make(map[eventingduckv1.SubscriberSpec]error)

	chanRef := eventingchannels.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	if len(subscriptions) == 0 || isFinalizer {
		s.logger.Sugar().Infof("Emptying subscriptions for channel Ref: %v", chanRef)
		subsMap, ok := s.subscriptions[chanRef]
		if !ok {
			// nothing to do
			s.logger.Sugar().Infof("No channel Ref %v found in subscriptions map", chanRef)
			return failedSubs, nil
		}
		for sub := range subsMap {
			s.unsubscribe(chanRef, sub)
		}
		delete(s.subscriptions, chanRef)
		return failedSubs, nil
	}

	activeSubs, ok := s.subscriptions[chanRef]
	if !ok {
		activeSubs = make(map[string]eventingduckv1.SubscriberSpec)
	}

	for _, sub := range subscriptions {
		// check if the subscription already exist and do nothing in this case
		if _, ok := activeSubs[string(sub.UID)]; ok {
			activeSubs[string(sub.UID)] = sub
			s.logger.Sugar().Infof("Subscription: %v already active for channel: %v", sub, chanRef)
			continue
		}
		// subscribe and update failedSubscription if subscribe fails
		err := s.subscribe(ctx, chanRef, string(sub.UID))
		if err != nil {
			s.logger.Sugar().Errorf("failed to subscribe (subscription:%q) to channel: %v. Error:%s", sub, chanRef, err.Error())
			failedSubs[sub] = err
			continue
		}
		activeSubs[string(sub.UID)] = sub
	}
	s.subscriptions[chanRef] = activeSubs
	// Unsubscribe for deleted subscriptions
	for _, sub := range subscriptions {
		if _, ok := activeSubs[string(sub.UID)]; !ok {
			s.unsubscribe(chanRef, string(sub.UID))
		}
	}
	// delete the channel from s.subscriptions if chMap is empty
	if len(s.subscriptions[chanRef]) == 0 {
		delete(s.subscriptions, chanRef)
	}
	return failedSubs, nil
}

func (s *KinesisDispatcher) subscribe(ctx context.Context, channel eventingchannels.ChannelReference, subID string) error {
	s.logger.Info("Subscribing to channel", zap.Any("channel", channel), zap.Any("subscription", subID))

	session, present := s.kinesisSessions[channel]
	if !present {
		s.logger.Error("Kinesis session not found", zap.Any("channel", channel))
		return fmt.Errorf("Kinesis session for channel %q not found", channel.String()) //nolint:stylecheck
	}
	iterators, err := kinesisutil.GetShardIterators(ctx, session.Client, &session.StreamName)
	if err != nil {
		s.logger.Error("Kinesis shard iterator request error", zap.Error(err))
		return fmt.Errorf("Kinesis shard iterator request error: %s", err) //nolint:stylecheck
	}
	for _, iterator := range iterators {
		go func(nextRecord *string, channel eventingchannels.ChannelReference, subID string) {
			for {
				subscription, exist := s.subscriptions[channel][subID]
				if !exist {
					s.logger.Info("Subscription not found, stopping message dispatcher")
					return
				}
				if nextRecord == nil {
					s.logger.Info("Null shard iterator, stop subscriber process. Is the stream closed?")
					return
				}
				records, err := kinesisutil.GetRecord(session.Client, nextRecord)
				if records != nil {
					nextRecord = records.NextShardIterator
				}
				if err != nil {
					s.logger.Error("Error reading Kinesis stream message", zap.Error(err))
					continue
				}

				for _, record := range records.Records {
					s.logger.Info("New shard records received", zap.Any("sub", subscription.SubscriberURI))
					e := event.New(event.CloudEventsVersionV1)
					err = e.UnmarshalJSON(record.Data)
					if err != nil {
						s.logger.Error("Can't decode event", zap.Error(err))
						continue
					}
					err = e.Validate()
					if err != nil {
						s.logger.Error("Event validation error", zap.Error(err))
						continue
					}
					_, err = s.dispatcher.DispatchMessage(
						context.Background(),
						binding.ToMessage(&e),
						nil,
						subscription.SubscriberURI.URL(),
						subscription.ReplyURI.URL(),
						nil,
					)
					if err != nil {
						s.logger.Error("Message dispatching error", zap.Error(err))
					}
				}
			}
		}(iterator, channel, subID)
	}
	return nil
}

// should be called only while holding subscriptionsMux.
func (s *KinesisDispatcher) unsubscribe(channel eventingchannels.ChannelReference, subscription string) {
	s.logger.Info("Unsubscribe from channel:", zap.Any("channel", channel), zap.Any("subscription", subscription))
	delete(s.subscriptions[channel], subscription)
}

// UpdateHostToChannelMap will be called from the controller that watches kinesis channels.
// It will update internal hostToChannelMap which is used to resolve the hostHeader of the
// incoming request to the correct ChannelReference in the receiver function.
func (s *KinesisDispatcher) UpdateHostToChannelMap(config *multichannelfanout.Config) error {
	if config == nil {
		return errors.New("nil config")
	}

	s.hostToChannelMapLock.Lock()
	defer s.hostToChannelMapLock.Unlock()

	hcMap, err := createHostToChannelMap(config)
	if err != nil {
		return err
	}

	s.setHostToChannelMap(hcMap)
	return nil
}

func (s *KinesisDispatcher) getHostToChannelMap() map[string]eventingchannels.ChannelReference {
	return s.hostToChannelMap.Load().(map[string]eventingchannels.ChannelReference)
}

func (s *KinesisDispatcher) setHostToChannelMap(hcMap map[string]eventingchannels.ChannelReference) {
	s.hostToChannelMap.Store(hcMap)
}

func createHostToChannelMap(config *multichannelfanout.Config) (map[string]eventingchannels.ChannelReference, error) {
	hcMap := make(map[string]eventingchannels.ChannelReference, len(config.ChannelConfigs))
	for _, cConfig := range config.ChannelConfigs {
		if cr, ok := hcMap[cConfig.HostName]; ok {
			return nil, fmt.Errorf(
				"duplicate hostName found. Each channel must have a unique host header. HostName:%s, channel:%s.%s, channel:%s.%s",
				cConfig.HostName,
				cConfig.Namespace,
				cConfig.Name,
				cr.Namespace,
				cr.Name)
		}
		hcMap[cConfig.HostName] = eventingchannels.ChannelReference{Name: cConfig.Name, Namespace: cConfig.Namespace}
	}
	return hcMap, nil
}

func (s *KinesisDispatcher) getChannelReferenceFromHost(host string) (eventingchannels.ChannelReference, error) {
	chMap := s.getHostToChannelMap()
	cr, ok := chMap[host]
	if !ok {
		return cr, fmt.Errorf("invalid HostName:%q. HostName not found in any of the watched kinesis channels", host)
	}
	return cr, nil
}

func (s *KinesisDispatcher) KinesisSessionExist(ctx context.Context, channel *v1alpha1.KinesisChannel) bool {
	s.mux.Lock()
	defer s.mux.Unlock()
	cRef := eventingchannels.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	_, present := s.kinesisSessions[cRef]
	return present
}

func (s *KinesisDispatcher) CreateKinesisSession(ctx context.Context, channel *v1alpha1.KinesisChannel, secret *corev1.Secret) error {
	s.mux.Lock()
	defer s.mux.Unlock()
	cRef := eventingchannels.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	_, present := s.kinesisSessions[cRef]
	if !present {
		client, err := s.kinesisClient(channel.Spec.AccountRegion, secret)
		if err != nil {
			return fmt.Errorf("error creating Kinesis session: %v", err)
		}
		s.kinesisSessions[cRef] = stream{
			StreamName: channel.Name,
			Client:     client,
		}
	}
	return nil
}

func (s *KinesisDispatcher) DeleteKinesisSession(ctx context.Context, channel *v1alpha1.KinesisChannel) {
	s.mux.Lock()
	defer s.mux.Unlock()
	cRef := eventingchannels.ChannelReference{Namespace: channel.Namespace, Name: channel.Name}
	delete(s.kinesisSessions, cRef)
}

func (s *KinesisDispatcher) kinesisClient(region string, creds *corev1.Secret) (*kinesis.Kinesis, error) {
	if creds == nil {
		return nil, fmt.Errorf("credentials data is nil")
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
