[![Go Report Card](https://goreportcard.com/badge/github.com/triggermesh/aws-kinesis-channel)](https://goreportcard.com/report/github.com/triggermesh/aws-kinesis-channel)

# AWS Kinesis Event Channel Controller

This Kubernetes controller provides [AWS Kinesis](https://aws.amazon.com/kinesis/) backed Event Channels. Use it instead of the Kafka or GCP PuSub for message persistence and to get events from AWS.

## Deploy and Development

Custom resource definition may be deployed by running following command:

```
kubectl apply -f release/
```

For development purpose we are using [ko](https://github.com/google/ko) tool which may be used to build and deploy CRD from sources:

```
ko apply -f config/
```

This will take all the configurations and deploy the AWS Kinesis CRD on your cluster to `knative-eventing` namespace. Change configurations if needed.

To see it's running use:

```
kubectl -n knative-eventing get pods -l messaging.triggermesh.dev/channel=kinesis-channel
```

## Usage

In order for a channel to be created on your AWS account, you need to create a kubernetes secret with `aws_access_key_id` and `aws_secret_access_key` keys with valid values:

```
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: Secret
metadata:
  name: awscreds
data:
  aws_access_key_id: <access key>
  aws_secret_access_key: <secret key>
EOF
```

Once the secret is created (e.g `awscreds`) you can deploy a AWS Kinesis Channel like so:

```
cat <<EOF | kubectl apply -f -
apiVersion: messaging.triggermesh.dev/v1alpha1
kind: KinesisChannel
metadata:
  name: aws-kinesis-test
spec:
  account_region: us-east-2
  account_creds: awscreds
EOF
```

As soon as `aws-kinesis-test` channel becomes ready you can subscribe to it with different services.

### Knative Eventing Broker

In order to use AWS Kinesis channels as a backend for Knative Eventing Broker you just need to create a Configmap with channel template spec. Assuming that your AWS credentials secret name is `awscreds`, here is a Configmap sample:

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: config-br-kinesis-channel
data:
  channelTemplateSpec: |
    apiVersion: messaging.triggermesh.dev/v1alpha1
    kind: KinesisChannel
    metadata:
      annotations:
        messaging.knative.dev/subscribable: v1beta1
    spec:
      account_region: us-east-2
      account_creds: awscreds
```

Broker object to utilize Kinesis channels in our case should look like this:

```
apiVersion: eventing.knative.dev/v1
kind: Broker
metadata:
  annotations:
    eventing.knative.dev/broker.class: MTChannelBasedBroker
  name: kinesis-default
spec:
  config:
    apiVersion: v1
    kind: ConfigMap
    name: config-br-kinesis-channel
```

## Support

We would love your feedback and help on this project, so don't hesitate to let us know what is wrong and how we could improve them, just file an [issue](https://github.com/triggermesh/aws-kinesis-channel/issues/new) or join those of use who are maintaining them and submit a [PR](https://github.com/triggermesh/aws-kinesis-channel/compare).

## Code of conduct

This project is by no means part of [CNCF](https://www.cncf.io/) but we abide by its [code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md).



