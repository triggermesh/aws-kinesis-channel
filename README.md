# AWS Kinesis Knative Provisioner

Cluster channel provisioner provides Knative channels for [AWS Kinesis](https://aws.amazon.com/kinesis/) Streams.


## Installation

Cluster channel provisioner consists of several components: channel controller and message dispatcher. Channel controller and message dispatcher must run together in the `knative-eventing` namespace

You can install Cluster channel provisioner by applying `aws-kinesis-channel-provisioner.yaml` manifest:

```
kubectl apply -f aws-kinesis-channel-provisioner.yaml
```
 this will install all required resources, cluster roles, services, etc.

Check that all provisioner pods are in `Running` state:

```
kubectl -n knative-eventing get pods --selector=clusterChannelProvisioner=aws-kinesis
```

Dispatcher pod may have an API connection errors on initialization so don't worry if you see a couple of restart in a status output.  


Please note that provided configurations has several hardcoded values, such as `AWS_REGION` which is set to `us-east-1` and `KINESIS_STREAM` value which is set to `triggermesh`. 
  

## Development

Development cycle is based on [ko](https://github.com/google/ko) tool - make your changes locally and deploy them with a single command:  

```
ko apply -f config/
```


## Support

We would love your feedback on this Cluster channel provisioner so don't hesitate to let us know what is wrong and how we could improve it, just file an [issue](https://github.com/triggermesh/aws-kinesis-provisioner/issues/new)

## Code of Conduct

This project is by no means part of [CNCF](https://www.cncf.io/) but we abide by its [code of conduct](https://github.com/cncf/foundation/blob/master/code-of-conduct.md)

