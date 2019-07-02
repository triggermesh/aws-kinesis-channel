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
	"github.com/knative/eventing/pkg/provisioners"
	"go.uber.org/zap"
)

// Connect creates a new Kinesis-Streaming connection
func Connect(accountAccessKeyID, accountSecretAccessKey, region string, logger *zap.SugaredLogger) (*kinesis.Kinesis, error) {
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
	logger.Infof("Connect(): connection to Kinesis established, Conn=%+v", kinesis)
	return kinesis, nil
}

func Exist(client *kinesis.Kinesis, streamName *string) (bool, error) {
	_, err := client.DescribeStream(&kinesis.DescribeStreamInput{
		StreamName: streamName,
	})
	if err != nil {
		return false, err
	}
	return true, nil
	//TODO maybe need to check stream status, if it is not ACTIVE, return false and no error, or
	// should probably return stream in the responce to check status and other params later in logic
}

func Create(client *kinesis.Kinesis, streamName *string) error {
	_, err := client.CreateStream(&kinesis.CreateStreamInput{
		ShardCount: aws.Int64(1), // by now creating streams with only one shard.
		StreamName: streamName,
	})
	if err != nil {
		return err
	}
	return nil
}

func Delete(client *kinesis.Kinesis, streamName *string) error {
	_, err := client.DeleteStream(&kinesis.DeleteStreamInput{
		EnforceConsumerDeletion: aws.Bool(true), // by now creating streams with only one shard.
		StreamName:              streamName,
	})
	if err != nil {
		return err
	}
	return nil
}

// Publish publishes msg to Kinesis stream
func Publish(client *kinesis.Kinesis, streamName *string, msg []byte, logger *zap.SugaredLogger) error {
	_, err := client.PutRecord(&kinesis.PutRecordInput{
		Data:         msg,
		PartitionKey: streamName,
		StreamName:   streamName,
	})
	return err
}

func GetRecord(client *kinesis.Kinesis, shardIterator *string) (provisioners.Message, *string, error) {
	var message provisioners.Message
	res, err := client.GetRecords(&kinesis.GetRecordsInput{
		Limit:         aws.Int64(1),
		ShardIterator: shardIterator,
	})
	if err != nil {
		return message, nil, err
	}
	message.Payload = res.Records[0].Data
	return message, res.NextShardIterator, nil
}
