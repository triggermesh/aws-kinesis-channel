# Custom Controller for Kinesis Sources


## Generating Code for Custom Controller 
In case of any changes to types for the custom controller, use the following commands to regenerate client and deepcopy files

```
ROOT_PACKAGE="github.com/triggermesh/aws-kinesis-provisioner"
CUSTOM_RESOURCE_NAME="kinesissource"
CUSTOM_RESOURCE_VERSION="v1"

go get -u k8s.io/code-generator/...
cd $GOPATH/src/k8s.io/code-generator

./generate-groups.sh all "$ROOT_PACKAGE/pkg/client" "$ROOT_PACKAGE/pkg/apis" "$CUSTOM_RESOURCE_NAME:$CUSTOM_RESOURCE_VERSION"

```

## Run localy 
First create custom resource definition in your cluster: 
```
kubectl apply -f crd/crd.yaml
```

To run the controller:
``` 
go build . 
./aws-kinesis-provisioner
```

And then in a separate shell, create custom resource:
```
kubectl create -f example/kinesis-source-example.yaml
```

As output you get the following logs when creating, updating or deleting custom resource:
```
INFO[0000] Successfully constructed k8s client          
INFO[0000] Controller.Run: initiating                   
INFO[0001] Controller.Run: cache sync complete          
INFO[0001] Controller.runWorker: starting                 
```

## Run with Docker 
Build the image with 
```
docker build -t kinesiscrd .
```

Create custom resource definition in your cluster: 
```
kubectl apply -f crd/crd.yaml
```

And then run it with the volumes to your `.kube` folder
```
docker run -v $HOME/.kube:/root/.kube/  kinesiscrd:latest
```
Get the following as output

```
time="2019-06-21T14:28:32Z" level=info msg="Successfully constructed k8s client"
time="2019-06-21T14:28:32Z" level=info msg="Controller.Run: initiating"
time="2019-06-21T14:28:33Z" level=info msg="Controller.Run: cache sync complete"
time="2019-06-21T14:28:33Z" level=info msg="Controller.runWorker: starting"
time="2019-06-21T14:28:33Z" level=info msg="Controller.processNextItem: start"
```

