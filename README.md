[![Go Report Card](https://goreportcard.com/badge/github.com/triggermesh/aws-kinesis-channel)](https://goreportcard.com/report/github.com/triggermesh/aws-kinesis-channel)

# AWS Kinesis Knative Custom Controller

Cluster channel provisioner provides Knative channels with [AWS Kinesis](https://aws.amazon.com/kinesis/) as message queue backend.

## Deploy

We are using [ko](https://github.com/google/ko) tool to deploy custom resources:
```
ko apply -f config/
```
This will take all the configurations and deploy AWS Kinesis CRD on your cluster to `knative-eventing` namespace. Change configurations if needed.

To see it's running use:
```
kubectl -n knative-eventing get pods -l messaging.triggermesh.dev/channel=kinesis-channel
```

## Usage

In order for a channel to connect to your AWS account, you should create k8s secret with `aws_access_key_id` and `aws_secret_access_key` keys with valid values. 

After secret was created you can deploy AWS Kinesis Channel:

```
cat <<EOF | kubectl apply -f -
apiVersion: messaging.triggermesh.dev/v1alpha1
kind: KinesisChannel
metadata:
  name: aws-kinesis-test
spec:
  stream_name: triggermesh-test-stream
  account_region: us-east-2
  account_creds: awscreds
EOF
```

As soon as aws-kinesis-test channel becomes ready you can subscribe to it with different services.

## Support

We would love your feedback and help on this project, so don't hesitate to let us know what is wrong and how we could improve them, just file an [issue](https://github.com/triggermesh/aws-kinesis-channel/issues/new) or join those of use who are maintaining them and submit a [PR](https://github.com/triggermesh/aws-kinesis-channel/compare).

## Code of conduct

This project is by no means part of [CNCF](https://www.cncf.io/) but we abide by its [code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).



