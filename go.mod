module github.com/triggermesh/aws-kinesis-channel

go 1.15

replace (
	k8s.io/api => k8s.io/api v0.16.8
	k8s.io/apimachinery => k8s.io/apimachinery v0.16.8
	k8s.io/client-go => k8s.io/client-go v0.16.8
	k8s.io/code-generator => k8s.io/code-generator v0.16.8
)

require (
	github.com/aws/aws-sdk-go v1.30.18
	github.com/cloudevents/sdk-go/v2 v2.0.0-RC2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/knative/eventing v0.14.2
	github.com/onsi/ginkgo v1.11.0 // indirect
	github.com/onsi/gomega v1.8.1 // indirect
	go.opencensus.io v0.22.3
	go.uber.org/zap v1.15.0
	k8s.io/api v0.18.2
	k8s.io/apiextensions-apiserver v0.18.2 // indirect
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/eventing v0.14.1-0.20200504081943-2f47aac0b6fd
	knative.dev/pkg v0.0.0-20200501005942-d980c0865972
)
