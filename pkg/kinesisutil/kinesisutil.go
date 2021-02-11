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

package kinesisutil

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
	"go.uber.org/zap"
)

// Connect creates a new Kinesis-Streaming connection.
func Connect(accountAccessKeyID, accountSecretAccessKey, region string, logger *zap.SugaredLogger) (*kinesis.Kinesis, error) {
	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accountAccessKeyID, accountSecretAccessKey, ""),
		MaxRetries:  aws.Int(5),
	})
	if err != nil {
		logger.Errorf("create new session failed: %v", err)
		return nil, err
	}
	return kinesis.New(sess), nil
}

// Describe accepts kinesis client and stream name and returns kinesis stream description.
func Describe(ctx context.Context, client *kinesis.Kinesis, streamName string) (*kinesis.DescribeStreamOutput, error) {
	return client.DescribeStreamWithContext(ctx, &kinesis.DescribeStreamInput{
		StreamName: &streamName,
	})
}

// Create function creates kinesis stream.
func Create(ctx context.Context, client *kinesis.Kinesis, streamName string, streamShards int64) error {
	_, err := client.CreateStreamWithContext(ctx, &kinesis.CreateStreamInput{
		ShardCount: aws.Int64(streamShards),
		StreamName: &streamName,
	})
	return err
}

// Wait function periodically checks Kinesis stream status and returns if it's "Active"
func Wait(ctx context.Context, client *kinesis.Kinesis, streamName string) error {
	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("Kinesis stream %v did not get Active status", streamName)
		case <-ticker.C:
			streamOutput, err := client.DescribeStreamWithContext(ctx, &kinesis.DescribeStreamInput{
				StreamName: &streamName,
			})
			if err != nil {
				return err
			}
			if streamOutput.StreamDescription != nil {
				if *streamOutput.StreamDescription.StreamStatus == kinesis.StreamStatusActive {
					return nil
				}
			}
		}
	}
}

// Delete function deletes kinesis stream by its name.
func Delete(ctx context.Context, client *kinesis.Kinesis, streamName string) error {
	_, err := client.DeleteStreamWithContext(ctx, &kinesis.DeleteStreamInput{
		EnforceConsumerDeletion: aws.Bool(true), // by now creating streams with only one shard.
		StreamName:              &streamName,
	})
	return err
}

// Publish publishes msg to Kinesis stream.
func Publish(ctx context.Context, client *kinesis.Kinesis, streamName string, msg []byte, logger *zap.SugaredLogger) error {
	_, err := client.PutRecordWithContext(ctx, &kinesis.PutRecordInput{
		Data:         msg,
		PartitionKey: &streamName,
		StreamName:   &streamName,
	})
	return err
}

// GetRecord retrieves one stream record by specified shard iterator.
func GetRecord(client *kinesis.Kinesis, shardIterator *string) (*kinesis.GetRecordsOutput, error) {
	return client.GetRecords(&kinesis.GetRecordsInput{
		// https://docs.aws.amazon.com/sdk-for-go/api/service/kinesis/#Kinesis.PutRecord
		// "Each shard can support writes up to 1,000 records per second,
		// Although it would be logical to set the limit to 1000 messages per request,
		// performance tests showed best numbers at limit values between 2000 and 5000.
		Limit:         aws.Int64(3000),
		ShardIterator: shardIterator,
	})
}

// GetShardIterators returns "latest" shard iterator for specified stream.
func GetShardIterators(ctx context.Context, client *kinesis.Kinesis, streamName *string) ([]*string, error) {
	res, err := client.DescribeStreamWithContext(ctx, &kinesis.DescribeStreamInput{
		StreamName: streamName,
	})
	if err != nil || res.StreamDescription == nil {
		return nil, fmt.Errorf("Kinesis stream description: %w", err) //nolint:stylecheck
	}
	iterators := []*string{}
	iteratorType := kinesis.ShardIteratorTypeLatest
	for _, shard := range res.StreamDescription.Shards {
		shardObject, err := client.GetShardIteratorWithContext(ctx, &kinesis.GetShardIteratorInput{
			StreamName:        res.StreamDescription.StreamName,
			ShardId:           shard.ShardId,
			ShardIteratorType: &iteratorType,
		})
		if err != nil {
			return nil, fmt.Errorf("Cannot get shard iterator: %w", err)
		}
		iterators = append(iterators, shardObject.ShardIterator)
	}
	return iterators, nil
}
