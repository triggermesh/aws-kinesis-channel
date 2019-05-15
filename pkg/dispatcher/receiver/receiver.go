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

package receiver

import (
	"github.com/knative/eventing/pkg/provisioners"
	provisioner "github.com/triggermesh/aws-kinesis-provisioner/pkg/kinesis"
	"go.uber.org/zap"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Receiver struct {
	logger  *zap.Logger
	client  client.Client
	kClient *provisioner.Client
}

func New(logger *zap.Logger, client client.Client) (*Receiver, []manager.Runnable) {
	kinesisClient, err := provisioner.NewClient()
	if err != nil {
		return nil, []manager.Runnable{}
	}
	r := &Receiver{
		logger: logger,
		client: client,

		kClient: kinesisClient,
	}
	return r, []manager.Runnable{r.newMessageReceiver()}
}

func (r *Receiver) newMessageReceiver() *provisioners.MessageReceiver {
	return provisioners.NewMessageReceiver(r.sendEventToTopic, r.logger.Sugar())
}

func (r *Receiver) sendEventToTopic(channel provisioners.ChannelReference, message *provisioners.Message) error {
	r.logger.Debug("received message")
	return r.kClient.PutMessageToStream(channel.Name, message.Payload)
}
