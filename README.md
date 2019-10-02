[![Go Report Card](https://goreportcard.com/badge/github.com/triggermesh/aws-kinesis-channel)](https://goreportcard.com/report/github.com/triggermesh/aws-kinesis-channel)

# AWS Kinesis Event Channel Controller

This Kubernetes controller provides [AWS Kinesis](https://aws.amazon.com/kinesis/) backed Event Channels. Use it instead of the Kafka or GCP PuSub for message persistency and to get events from AWS.

## Deploy and Development

Custom resource definition may be deployed by running following command:

```
kubectl apply -f config/
```

This will take all the configurations and deploy the AWS Kinesis CRD on your cluster to `knative-eventing` namespace. Change configurations if needed.

To see it's running use:

```
kubectl -n knative-eventing get pods -l messaging.triggermesh.dev/channel=kinesis-channel
```

## Usage

In order for a channel to be created on our AWS account, you need to create a kubernetes secret with `aws_access_key_id` and `aws_secret_access_key` keys with valid values. 

Once the secret is created (e.g `awscreds`) you can deploy a AWS Kinesis Channel like so:

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

As soon as `aws-kinesis-test` channel becomes ready you can subscribe to it with different services.

## Support

We would love your feedback and help on this project, so don't hesitate to let us know what is wrong and how we could improve them, just file an [issue](https://github.com/triggermesh/aws-kinesis-channel/issues/new) or join those of use who are maintaining them and submit a [PR](https://github.com/triggermesh/aws-kinesis-channel/compare).

## Code of conduct

This project is by no means part of [CNCF](https://www.cncf.io/) but we abide by its [code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).



