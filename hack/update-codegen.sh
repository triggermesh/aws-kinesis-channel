#!/usr/bin/env bash

vendor/k8s.io/code-generator/generate-groups.sh all \
github.com/triggermesh/aws-kinesis-provisioner/pkg/generated \
github.com/triggermesh/aws-kinesis-provisioner/pkg/apis \
"kinesiscontroller:v1alpha1" --go-header-file hack/custom-boilerplate.go.txt 