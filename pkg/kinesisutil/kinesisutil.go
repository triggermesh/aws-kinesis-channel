/*
 * Copyright 2018 The Knative Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kinesisutil

import (
	"errors"

	"github.com/aws/aws-sdk-go/service/kinesis"
	"go.uber.org/zap"
)

// Connect creates a new Kinesis-Streaming connection
func Connect(client *kinesis.Kinesis, consumerARN, shardID *string, logger *zap.SugaredLogger) (*kinesis.SubscribeToShardEventStream, error) {
	shardSubscription, err := client.SubscribeToShard(&kinesis.SubscribeToShardInput{
		ConsumerARN: consumerARN,
		ShardId:     shardID,
	})
	if err != nil {
		logger.Errorf("Connect(): create new connection to kinesis stream failed: %v", err)
		return nil, err
	}
	logger.Infof("Connect(): connection to Kinesis stream established, Conn=%+v", shardSubscription)
	return shardSubscription.EventStream, nil
}

// Close must be the last call to close the connection
func Close(streamConn *kinesis.SubscribeToShardEventStream, logger *zap.SugaredLogger) (err error) {

	if streamConn == nil {
		err = errors.New("can't close empty connection")
		return
	}
	err = (*streamConn).Close()
	if err != nil {
		logger.Errorf("Can't close connection: %+v", err)
	}
	return
}

// Publish a message to a subject
func Publish(client *kinesis.Kinesis, streamName, partitionKey *string, msg []byte, logger *zap.SugaredLogger) (err error) {
	_, err = client.PutRecord(&kinesis.PutRecordInput{
		Data:         msg,
		PartitionKey: partitionKey,
		StreamName:   streamName,
	})
	if err != nil {
		logger.Errorf("Can't publish message to kinesis: %+v", err)
	}
	return
}
