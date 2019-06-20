# Generating Code for Custom Controller 

```
ROOT_PACKAGE="github.com/triggermesh/aws-kinesis-provisioner"

CUSTOM_RESOURCE_NAME="kinesissource"

CUSTOM_RESOURCE_VERSION="v1"

go get -u k8s.io/code-generator/...
cd $GOPATH/src/k8s.io/code-generator

./generate-groups.sh all "$ROOT_PACKAGE/pkg/client" "$ROOT_PACKAGE/pkg/apis" "$CUSTOM_RESOURCE_NAME:$CUSTOM_RESOURCE_VERSION"

```