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
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
	"go.uber.org/zap"
)

// Connect creates a new Kinesis-Streaming connection
func Connect(accountAccessKeyID, accountSecretAccessKey, region, streamName string, logger *zap.SugaredLogger) (*kinesis.Kinesis, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accountAccessKeyID, accountSecretAccessKey, ""),
		MaxRetries:  aws.Int(5),
	})
	if err != nil {
		logger.Errorf("Connect(): create new session failed: %v", err)
		return nil, err
	}

	kinesis := kinesis.New(sess)
	logger.Infof("Connect(): connection to Kinesis stream established, Conn=%+v", kinesis)
	return kinesis, nil
}

// Publish publishes msg to Kinesis
func Publish(client *kinesis.Kinesis, partitionKey, streamName *string, msg []byte, logger *zap.SugaredLogger) error {
	_, err := client.PutRecord(&kinesis.PutRecordInput{
		Data:         msg,
		PartitionKey: partitionKey,
		StreamName:   streamName,
	})
	return err
}
