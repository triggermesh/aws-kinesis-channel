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

package kinesis

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kinesis"
	"github.com/knative/eventing/pkg/provisioners"
)

// Client contains Kinesis client to get API calls to AWS Kinesis
type Client struct {
	Kinesis *kinesis.Kinesis
}

// NewClient initializes new client
func NewClient() (*Client, error) {
	var client Client

	accountAccessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	accountSecretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	region := os.Getenv("AWS_REGION")

	sess, err := session.NewSession(&aws.Config{
		Region:      aws.String(region),
		Credentials: credentials.NewStaticCredentials(accountAccessKeyID, accountSecretAccessKey, ""),
		MaxRetries:  aws.Int(5),
	})
	if err != nil {
		return nil, err
	}

	client.Kinesis = kinesis.New(sess)
	return &client, err
}

//DescribeKinesisStream first creates then describes kinesis stream.
func (c *Client) DescribeKinesisStream(streamName string) (*kinesis.StreamDescription, error) {
	c.CreateKinesisStream(streamName)
	stream, err := c.Kinesis.DescribeStream(&kinesis.DescribeStreamInput{
		StreamName: aws.String(streamName),
	})
	return stream.StreamDescription, err
}

//CreateKinesisStream creates stream with a steam name
func (c *Client) CreateKinesisStream(streamName string) error {
	_, err := c.Kinesis.CreateStream(&kinesis.CreateStreamInput{
		ShardCount: aws.Int64(1),
		StreamName: aws.String(streamName),
	})
	return err
}

//DeleteKinesisStream deletes selected stream
func (c *Client) DeleteKinesisStream(enforceConsumerDeletion bool, streamName string) error {
	_, err := c.Kinesis.DeleteStream(&kinesis.DeleteStreamInput{
		EnforceConsumerDeletion: aws.Bool(enforceConsumerDeletion),
		StreamName:              aws.String(streamName),
	})
	return err
}

//GetStreamMessage gets messages up to 10,000 from a specified stream shard
func (c *Client) GetStreamMessage(shardIterator *string) ([]provisioners.Message, *string, error) {
	// Get info about a particular stream
	messages := []provisioners.Message{}
	var nextShardIterator *string

	// set records output limit. Should not be more than 10000, othervise panics
	input := kinesis.GetRecordsInput{
		ShardIterator: shardIterator,
	}

	recordsOutput, err := c.Kinesis.GetRecords(&input)
	if err != nil {
		return messages, nextShardIterator, err
	}

	for _, record := range recordsOutput.Records {
		msg := provisioners.Message{}
		msg.Payload = record.Data
		msg.Headers = map[string]string{
			"EncryptionType": *recordsOutput.Records[0].EncryptionType,
			"PartitionKey":   *recordsOutput.Records[0].PartitionKey,
			"SequenceNumber": *recordsOutput.Records[0].SequenceNumber,
		}
		messages = append(messages, msg)
	}

	nextShardIterator = recordsOutput.NextShardIterator

	return messages, nextShardIterator, nil
}

//PutMessageToStream puts messages to the indicated stream
func (c *Client) PutMessageToStream(streamName string, messagePayload []byte) error {

	_, err := c.Kinesis.PutRecord(&kinesis.PutRecordInput{
		Data:         messagePayload,
		PartitionKey: aws.String("1"),
		StreamName:   aws.String(streamName),
	})

	return err
}
