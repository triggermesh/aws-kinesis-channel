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

## Run 
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
INFO[0001] Controller.processNextItem: start            
INFO[0198] Add kinesissource: default/anatoliy-aws-kinesis 
INFO[0198] Controller.processNextItem: object created detected: default/anatoliy-aws-kinesis 
INFO[0198] TestHandler.ObjectCreated                    
INFO[0198] Controller.runWorker: processing next item   
INFO[0198] Controller.processNextItem: start            
INFO[0221] Update kinesissource: default/anatoliy-aws-kinesis 
INFO[0221] Controller.processNextItem: object created detected: default/anatoliy-aws-kinesis 
INFO[0221] TestHandler.ObjectCreated                    
INFO[0221] Controller.runWorker: processing next item   
INFO[0221] Controller.processNextItem: start            
INFO[0241] Delete kinesissource: default/anatoliy-aws-kinesis 
INFO[0241] Controller.processNextItem: object deleted detected: default/anatoliy-aws-kinesis 
INFO[0241] TestHandler.ObjectDeleted                    
INFO[0241] Controller.runWorker: processing next item   
INFO[0241] Controller.processNextItem: start  
```

