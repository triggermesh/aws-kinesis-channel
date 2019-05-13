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
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/event"

	eventingv1alpha1 "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	"github.com/knative/eventing/pkg/provisioners"
	"github.com/triggermesh/aws-kinesis-provisioner/pkg/kinesis"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerAgentName = "aws-kinesis-channel-dispatcher"
)

func New(mgr manager.Manager, logger *zap.Logger) (controller.Controller, error) {
	// reconcileChan is used when the dispatcher itself needs to force reconciliation of a Channel.
	reconcileChan := make(chan event.GenericEvent)

	client, err := kinesis.NewClient()
	if err != nil {
		return nil, err
	}

	r := &reconciler{
		recorder: mgr.GetRecorder(controllerAgentName),
		logger:   logger,

		dispatcher:    provisioners.NewMessageDispatcher(logger.Sugar()),
		reconcileChan: reconcileChan,

		channelLock: sync.RWMutex{},
		channels:    map[channelName]*receiver{},

		kClient: client,
	}

	c, err := controller.New(controllerAgentName, mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		logger.Error("Unable to create controller.", zap.Error(err))
		return nil, err
	}

	// Watch Channels.
	err = c.Watch(&source.Kind{
		Type: &eventingv1alpha1.Channel{},
	}, &handler.EnqueueRequestForObject{})
	if err != nil {
		logger.Error("Unable to watch Channels.", zap.Error(err), zap.Any("type", &eventingv1alpha1.Channel{}))
		return nil, err
	}

	// that Channel again.
	err = c.Watch(&source.Channel{
		Source: reconcileChan,
	}, &handler.EnqueueRequestForObject{})
	if err != nil {
		logger.Error("Unable to watch the reconcile Channel", zap.Error(err))
		return nil, err
	}

	return c, nil
}
