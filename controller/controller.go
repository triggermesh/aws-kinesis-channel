/*
Copyright 2017 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"time"

	eventingApi "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	sourcesApi "github.com/knative/eventing/pkg/apis/sources/v1alpha1"

	eventingClient "github.com/knative/eventing/pkg/client/clientset/versioned"
	log "github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	kinesisv1 "github.com/triggermesh/aws-kinesis-provisioner/pkg/apis/kinesissource/v1"
	clientset "github.com/triggermesh/aws-kinesis-provisioner/pkg/client/clientset/versioned"
	informers "github.com/triggermesh/aws-kinesis-provisioner/pkg/client/informers/externalversions/kinesissource/v1"
	listers "github.com/triggermesh/aws-kinesis-provisioner/pkg/client/listers/kinesissource/v1"
)

const controllerAgentName = "kinesis-controller"

const (
	// SuccessSynced is used as part of the Event 'reason' when a KinesisSource is synced
	SuccessSynced = "Synced"
	// ErrResourceExists is used as part of the Event 'reason' when a KinesisSource fails
	// to sync due to a Deployment of the same name already existing.
	ErrResourceExists = "ErrResourceExists"

	// MessageResourceExists is the message used for Events when a resource
	// fails to sync due to a Deployment already existing
	MessageResourceExists = "Resource %q already exists and is not managed by KinesisSource"
	// MessageResourceSynced is the message used for an Event fired when a KinesisSource
	// is synced successfully
	MessageResourceSynced = "KinesisSource synced successfully"
)

// Controller is the controller implementation for KinesisSource resources
type Controller struct {
	// kubeclientset is a standard kubernetes clientset
	kubeclientset kubernetes.Interface
	// knativeclientset is a standard kubernetes clientset
	eventingclientset eventingClient.Interface
	// kinesisclientset is a clientset for our own API group
	kinesisclientset clientset.Interface

	kinesisSourcesLister listers.KinesisSourceLister
	kinesisSourcesSynced cache.InformerSynced

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface
}

// NewController returns a new kinesis controller
func NewController(kubeclientset kubernetes.Interface,
	eventingclientset eventingClient.Interface,
	kinesisclientset clientset.Interface,
	kinesisSourceInformer informers.KinesisSourceInformer) *Controller {

	controller := &Controller{
		kubeclientset:        kubeclientset,
		kinesisclientset:     kinesisclientset,
		eventingclientset:    eventingclientset,
		kinesisSourcesLister: kinesisSourceInformer.Lister(),
		kinesisSourcesSynced: kinesisSourceInformer.Informer().HasSynced,
		workqueue:            workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "KinesisSources"),
	}

	log.Info("Setting up event handlers")
	// Set up an event handler for when KinesisSource resources change
	kinesisSourceInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: controller.enqueueKinesisSource,
		UpdateFunc: func(old, new interface{}) {
			controller.enqueueKinesisSource(new)
		},
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	log.Info("Starting KinesisSource controller")

	// Wait for the caches to be synced before starting workers
	log.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.kinesisSourcesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	log.Info("Starting workers")
	// Launch two workers to process KinesisSource resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	log.Info("Started workers")
	<-stopCh
	log.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// KinesisSource resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		log.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the KinesisSource resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the KinesisSource resource with this namespace/name
	kinesisSource, err := c.kinesisSourcesLister.KinesisSources(namespace).Get(name)
	if err != nil {
		// The KinesisSource resource may no longer exist, in which case we stop
		// processing.
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("kinesisSource '%s' in work queue no longer exists", key))
			return nil
		}

		return err
	}

	ksname := kinesisSource.Spec.Name
	if ksname == "" {
		// We choose to absorb the error here as the worker would requeue the
		// resource otherwise. Instead, the next time the resource is updated
		// the resource will be queued again.
		utilruntime.HandleError(fmt.Errorf("%s: name must be specified", key))
		return nil
	}

	channel, err := c.eventingclientset.EventingV1alpha1().Channels(kinesisSource.Namespace).Create(newChannel(kinesisSource))
	if k8sErrors.IsAlreadyExists(err) {
		return nil
		// channel, err := c.eventingclientset.EventingV1alpha1().Channels(kinesisSource.Namespace).Get(ksname, metav1.GetOptions{})
		// if err != nil {
		// 	return err
		// }
		// need to update channel properties here
		//_, err = c.eventingclientset.EventingV1alpha1().Channels(kinesisSource.Namespace).Update(newChannel(kinesisSource))
	}

	_, err = c.eventingclientset.Sources().ContainerSources(kinesisSource.Namespace).Create(newContainerSource(kinesisSource, channel))
	if k8sErrors.IsAlreadyExists(err) {
		return nil
	}

	return err
}

// enqueueKinesisSource takes a KinesisSource resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than KinesisSource.
func (c *Controller) enqueueKinesisSource(obj interface{}) {
	var key string
	var err error
	if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

// handleObject will take any resource implementing metav1.Object and attempt
// to find the KinesisSource resource that 'owns' it. It does this by looking at the
// objects metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that KinesisSource resource to be processed. If the object does not
// have an appropriate OwnerReference, it will simply be skipped.
func (c *Controller) handleObject(obj interface{}) {
	var object metav1.Object
	var ok bool
	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object, invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("error decoding object tombstone, invalid type"))
			return
		}
		log.Infof("Recovered deleted object '%s' from tombstone", object.GetName())
	}
	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a KinesisSource, we should not do anything more
		// with it.
		if ownerRef.Kind != "KinesisSource" {
			return
		}

		kinesisSource, err := c.kinesisSourcesLister.KinesisSources(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			log.Infof("ignoring orphaned object '%s' of kinesisSource '%s'", object.GetSelfLink(), ownerRef.Name)
			return
		}

		c.enqueueKinesisSource(kinesisSource)
		return
	}
}

// newChannel creates a new Channel for a KinesisSource resource. It also sets
// the appropriate OwnerReferences on the resource so handleObject can discover
// the KinesisSource resource that 'owns' it.
func newChannel(kinesisSource *kinesisv1.KinesisSource) *eventingApi.Channel {
	return &eventingApi.Channel{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Channel",
			APIVersion: "eventing.knative.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kinesisSource.Name,
			Namespace: kinesisSource.Namespace,
		},
		Spec: eventingApi.ChannelSpec{
			Provisioner: &corev1.ObjectReference{
				APIVersion: "eventing.knative.dev/v1alpha1",
				Kind:       "ClusterChannelProvisioner",
				Name:       "kinesis-source-channel",
			},
		},
	}
}

// newChannel creates a new Channel for a KinesisSource resource. It also sets
// the appropriate OwnerReferences on the resource so handleObject can discover
// the KinesisSource resource that 'owns' it.
func newContainerSource(kinesisSource *kinesisv1.KinesisSource, channel *eventingApi.Channel) *sourcesApi.ContainerSource {
	return &sourcesApi.ContainerSource{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ContainerSource",
			APIVersion: "sources.eventing.knative.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      kinesisSource.Name,
			Namespace: kinesisSource.Namespace,
		},
		Spec: sourcesApi.ContainerSourceSpec{
			Image: "gcr.io/triggermesh/awskinesis:latest",
			Sink: &corev1.ObjectReference{
				APIVersion: channel.APIVersion,
				Name:       channel.Name,
				Kind:       channel.Kind,
			},
			Env: []corev1.EnvVar{
				corev1.EnvVar{
					Name: "AWS_ACCESS_KEY_ID",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							corev1.LocalObjectReference{
								Name: kinesisSource.Spec.AccountCreds,
							},
							"aws_access_key_id",
							new(bool),
						},
					},
				},
				corev1.EnvVar{
					Name: "AWS_SECRET_ACCESS_KEY",
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							corev1.LocalObjectReference{
								Name: kinesisSource.Spec.AccountCreds,
							},
							"aws_secret_access_key",
							new(bool),
						},
					},
				},
				corev1.EnvVar{
					Name:  "AWS_REGION",
					Value: kinesisSource.Spec.AccountRegion,
				},
				corev1.EnvVar{
					Name:  "Stream",
					Value: kinesisSource.Spec.StreamName,
				},
			},
		},
	}
}
