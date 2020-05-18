/*
Copyright (c) 2018 TriggerMesh, Inc

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
	"context"
	"fmt"
	"reflect"

	"github.com/aws/aws-sdk-go/service/kinesis"
	"github.com/kelseyhightower/envconfig"
	"github.com/knative/eventing/pkg/apis/messaging"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	eventingclientset "knative.dev/eventing/pkg/client/clientset/versioned"
	eventingClient "knative.dev/eventing/pkg/client/injection/client"
	"knative.dev/eventing/pkg/logging"
	"knative.dev/eventing/pkg/reconciler/names"
	"knative.dev/pkg/apis"
	kubeclient "knative.dev/pkg/client/injection/kube/client"
	"knative.dev/pkg/client/injection/kube/informers/apps/v1/deployment"
	"knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	"knative.dev/pkg/client/injection/kube/informers/core/v1/service"
	"knative.dev/pkg/client/injection/kube/informers/core/v1/serviceaccount"
	"knative.dev/pkg/client/injection/kube/informers/rbac/v1/rolebinding"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"

	"github.com/triggermesh/aws-kinesis-channel/pkg/apis/messaging/v1alpha1"
	kinesisclientset "github.com/triggermesh/aws-kinesis-channel/pkg/client/clientset/internalclientset"
	"github.com/triggermesh/aws-kinesis-channel/pkg/client/clientset/internalclientset/scheme"
	kinesisclient "github.com/triggermesh/aws-kinesis-channel/pkg/client/injection/client"
	"github.com/triggermesh/aws-kinesis-channel/pkg/client/injection/informers/messaging/v1alpha1/kinesischannel"
	kinesisChannelReconciler "github.com/triggermesh/aws-kinesis-channel/pkg/client/injection/reconciler/messaging/v1alpha1/kinesischannel"
	listers "github.com/triggermesh/aws-kinesis-channel/pkg/client/listers/messaging/v1alpha1"
	"github.com/triggermesh/aws-kinesis-channel/pkg/kinesisutil"
	"github.com/triggermesh/aws-kinesis-channel/pkg/reconciler/controller/resources"
)

const (
	// controllerAgentName is the string used by this controller to identify
	// itself when creating events.
	controllerAgentName = "kinesis-ch-controller"

	dispatcherDeploymentCreated     = "DispatcherDeploymentCreated"
	dispatcherDeploymentUpdated     = "DispatcherDeploymentUpdated"
	dispatcherDeploymentFailed      = "DispatcherDeploymentFailed"
	dispatcherServiceCreated        = "DispatcherServiceCreated"
	dispatcherServiceFailed         = "DispatcherServiceFailed"
	dispatcherServiceAccountCreated = "DispatcherServiceAccountCreated"
	dispatcherRoleBindingCreated    = "DispatcherRoleBindingCreated"

	// Name of the corev1.Events emitted from the reconciliation process.
	channelReconciled         = "ChannelReconciled"
	channelReconcileFailed    = "ChannelReconcileFailed"
	channelUpdateStatusFailed = "ChannelUpdateStatusFailed"
)

type envConfig struct {
	Image string `envconfig:"DISPATCHER_IMAGE" required:"true"`
}

func newDeploymentWarn(err error) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeWarning, "DispatcherDeploymentFailed", "Reconciling dispatcher Deployment failed with: %s", err)
}

func newDispatcherServiceWarn(err error) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeWarning, "DispatcherServiceFailed", "Reconciling dispatcher Service failed with: %s", err)
}

func newServiceAccountWarn(err error) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeWarning, "DispatcherServiceAccountFailed", "Reconciling dispatcher ServiceAccount failed: %s", err)
}

func newRoleBindingWarn(err error) pkgreconciler.Event {
	return pkgreconciler.NewEvent(corev1.EventTypeWarning, "DispatcherRoleBindingFailed", "Reconciling dispatcher RoleBinding failed: %s", err)
}

func init() {
	// Add run types to the default Kubernetes Scheme so Events can be
	// logged for run types.
	_ = scheme.AddToScheme(scheme.Scheme)
}

// Reconciler reconciles Kinesis Channels.
type Reconciler struct {
	KubeClientSet kubernetes.Interface

	EventingClientSet eventingclientset.Interface

	dispatcherImage string

	kinesischannelLister   listers.KinesisChannelLister
	kinesischannelInformer cache.SharedIndexInformer
	kinesisClientSet       kinesisclientset.Interface

	deploymentLister     appsv1listers.DeploymentLister
	serviceLister        corev1listers.ServiceLister
	endpointsLister      corev1listers.EndpointsLister
	serviceAccountLister corev1listers.ServiceAccountLister
	roleBindingLister    rbacv1listers.RoleBindingLister
}

var (
	dispatcherName = "kinesis-ch-dispatcher"
)

// NewController initializes the controller and is called by the generated code.
// Registers event handlers to enqueue events.
func NewController(
	ctx context.Context,
	cmw configmap.Watcher,
) *controller.Impl {

	kinesisChannelInformer := kinesischannel.Get(ctx)
	deploymentInformer := deployment.Get(ctx)
	serviceInformer := service.Get(ctx)
	endpointsInformer := endpoints.Get(ctx)
	serviceAccountInformer := serviceaccount.Get(ctx)
	roleBindingInformer := rolebinding.Get(ctx)

	r := &Reconciler{
		KubeClientSet:     kubeclient.Get(ctx),
		EventingClientSet: eventingClient.Get(ctx),

		kinesischannelLister:   kinesisChannelInformer.Lister(),
		kinesischannelInformer: kinesisChannelInformer.Informer(),
		kinesisClientSet:       kinesisclient.Get(ctx),
		deploymentLister:       deploymentInformer.Lister(),
		serviceLister:          serviceInformer.Lister(),
		endpointsLister:        endpointsInformer.Lister(),
		serviceAccountLister:   serviceAccountInformer.Lister(),
		roleBindingLister:      roleBindingInformer.Lister(),
	}

	env := &envConfig{}
	if err := envconfig.Process("", env); err != nil {
		logging.FromContext(ctx).Sugar().Panicf("unable to process Kinesis channel's required environment variables: %v", err)
	}

	if env.Image == "" {
		logging.FromContext(ctx).Panic("unable to process Kinesis channel's required environment variables (missing DISPATCHER_IMAGE)")
	}

	r.dispatcherImage = env.Image

	impl := kinesisChannelReconciler.NewImpl(ctx, r)

	logging.FromContext(ctx).Info("Setting up event handlers")

	filterFn := controller.FilterWithName(dispatcherName)
	// Call GlobalResync on kinesischannels.
	grCh := func(obj interface{}) {
		impl.GlobalResync(kinesisChannelInformer.Informer())
	}

	kinesisChannelInformer.Informer().AddEventHandler(controller.HandleAll(impl.Enqueue))

	// Set up watches for dispatcher resources we care about, since any changes to these
	// resources will affect our Channels. So, set up a watch here, that will cause
	// a global Resync for all the channels to take stock of their health when these change.
	deploymentInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterFn,
		Handler:    controller.HandleAll(grCh),
	})
	serviceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterFn,
		Handler:    controller.HandleAll(grCh),
	})
	endpointsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterFn,
		Handler:    controller.HandleAll(grCh),
	})
	serviceAccountInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterFn,
		Handler:    controller.HandleAll(grCh),
	})
	roleBindingInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: filterFn,
		Handler:    controller.HandleAll(grCh),
	})

	return impl
}

// ReconcileKind compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the KinesisChannel resource
// with the current status of the resource.
func (r *Reconciler) ReconcileKind(ctx context.Context, kc *v1alpha1.KinesisChannel) pkgreconciler.Event {
	kc.Status.InitializeConditions()

	logger := logging.FromContext(ctx)

	// Reconcile KinesisChannels and then write back any status updates regardless of
	// whether the reconcile error out.
	reconcileErr := r.reconcile(ctx, kc)
	if reconcileErr != nil {
		logger.Error("Error reconciling KinesisChannel", zap.Error(reconcileErr))
	} else {
		logger.Debug("KinesisChannel reconciled")
	}

	// Requeue if the resource is not ready
	return reconcileErr
}

func (r *Reconciler) FinalizeKind(ctx context.Context, kc *v1alpha1.KinesisChannel) pkgreconciler.Event {
	logger := logging.FromContext(ctx).Sugar()

	if kc.Spec.AccountCreds == "" {
		return nil
	}
	creds, err := r.KubeClientSet.CoreV1().Secrets(kc.Namespace).Get(kc.Spec.AccountCreds, metav1.GetOptions{})
	if err != nil {
		logger.Errorf("can't get account cred secret: %s", err)
		// we don't want to hang forever if someone removed secret
		return nil
	}
	kclient, err := r.kinesisClient(ctx, kc.Name, kc.Spec.AccountRegion, creds)
	if err != nil {
		logger.Errorf("can't create kinesis client: %s", err)
		// same here, give up if kinesis client cannot be created
		return nil
	}
	if err := r.removeKinesisStream(ctx, kc.Name, kclient); err != nil {
		logger.Errorf("can't remove kinesis stream: %s", err)
		// same, ignore missing resources
		return nil
	}
	return nil
}

func (r *Reconciler) reconcile(ctx context.Context, kc *v1alpha1.KinesisChannel) error {
	logger := logging.FromContext(ctx)

	// set channelable version annotation
	err := r.setAnnotations(ctx, kc)
	if err != nil {
		return fmt.Errorf("channel annotations update: %s", err)
	}

	// We reconcile the status of the Channel by looking at:
	// 1. Dispatcher Deployment for it's readiness.
	// 2. Dispatcher k8s Service for it's existence.
	// 3. Dispatcher endpoints to ensure that there's something backing the Service.
	// 4. K8s service representing the channel that will use ExternalName to point to the Dispatcher k8s service.

	// Make sure the dispatcher deployment exists and propagate the status to the Channel
	_, err = r.reconcileDispatcher(ctx, kc)
	if err != nil {
		return fmt.Errorf("reconcile dispatcher: %s", err)
	}

	_, err = r.reconcileDispatcherService(ctx, kc)
	if err != nil {
		return fmt.Errorf("reconcile dispatcher service: %s", err)
	}

	// Get the Dispatcher Service Endpoints and propagate the status to the Channel
	// endpoints has the same name as the service, so not a bug.
	e, err := r.endpointsLister.Endpoints(kc.GetNamespace()).Get(dispatcherName)
	if err != nil {
		if apierrs.IsNotFound(err) {
			kc.Status.MarkEndpointsFailed("DispatcherEndpointsDoesNotExist", "Dispatcher Endpoints does not exist")
		} else {
			logger.Error("Unable to get the dispatcher endpoints", zap.Error(err))
			kc.Status.MarkEndpointsFailed("DispatcherEndpointsGetFailed", "Failed to get dispatcher endpoints")
		}
		return fmt.Errorf("can't get endpoints: %s", err)
	}

	if len(e.Subsets) == 0 {
		logger.Error("No endpoints found for Dispatcher service", zap.Error(err))
		kc.Status.MarkEndpointsFailed("DispatcherEndpointsNotReady", "There are no endpoints ready for Dispatcher service")
		return fmt.Errorf("there are no endpoints ready for Dispatcher service %s", dispatcherName)
	}
	kc.Status.MarkEndpointsTrue()

	// Reconcile the k8s service representing the actual Channel. It points to the Dispatcher service via ExternalName
	svc, err := r.reconcileChannelService(ctx, kc)
	if err != nil {
		return fmt.Errorf("reconcile channel service: %s", err)
	}

	kc.Status.MarkChannelServiceTrue()
	kc.Status.SetAddress(&apis.URL{
		Scheme: "http",
		Host:   names.ServiceHostName(svc.Name, svc.Namespace),
	})

	if kc.Status.GetCondition(v1alpha1.KinesisChannelConditionStreamReady).IsUnknown() ||
		kc.Status.GetCondition(v1alpha1.KinesisChannelConditionStreamReady).IsFalse() {
		creds, err := r.KubeClientSet.CoreV1().Secrets(kc.Namespace).Get(kc.Spec.AccountCreds, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("can't get account cred secret: %s", err)
		}
		kclient, err := r.kinesisClient(ctx, kc.Name, kc.Spec.AccountRegion, creds)
		if err != nil {
			return fmt.Errorf("can't create kinesis client: %s", err)
		}
		if err := r.setupKinesisStream(ctx, kc.Name, kclient); err != nil {
			return fmt.Errorf("can't create kinesis stream: %s", err)
		}
	}
	kc.Status.MarkStreamTrue()
	return nil
}

func (r *Reconciler) setAnnotations(ctx context.Context, kc *v1alpha1.KinesisChannel) error {
	annotations := kc.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	if version, present := annotations[messaging.SubscribableDuckVersionAnnotation]; !present || version != "v1beta1" {
		// explicitly set subscribable version
		// https://github.com/knative/eventing/blob/master/docs/spec/channel.md#annotation-requirements
		annotations[messaging.SubscribableDuckVersionAnnotation] = "v1beta1"
		kc.SetAnnotations(annotations)
		_, err := r.kinesisClientSet.MessagingV1alpha1().KinesisChannels(kc.Namespace).Update(kc)
		return err
	}
	return nil
}

func (r *Reconciler) reconcileDispatcher(ctx context.Context, kc *v1alpha1.KinesisChannel) (*appsv1.Deployment, error) {
	// Configure RBAC in namespace to access the configmaps
	sa, err := r.reconcileServiceAccount(ctx, kc)
	if err != nil {
		return nil, err
	}

	_, err = r.reconcileRoleBinding(ctx, dispatcherName, kc, dispatcherName, sa)
	if err != nil {
		return nil, err
	}

	// Reconcile the RoleBinding allowing read access to the shared configmaps.
	// Note this RoleBinding is created in the system namespace and points to a
	// subject in the dispatcher's namespace.
	// TODO: might change when ConfigMapPropagation lands
	roleBindingName := fmt.Sprintf("%s-%s", dispatcherName, kc.GetNamespace())
	_, err = r.reconcileRoleBinding(ctx, roleBindingName, kc, "eventing-config-reader", sa)
	if err != nil {
		return nil, err
	}
	args := resources.DispatcherArgs{
		DispatcherNamespace: kc.GetNamespace(),
		Image:               r.dispatcherImage,
	}

	expected := resources.MakeDispatcher(args)
	d, err := r.deploymentLister.Deployments(kc.GetNamespace()).Get(dispatcherName)
	if err != nil {
		if apierrs.IsNotFound(err) {
			d, err := r.KubeClientSet.AppsV1().Deployments(kc.GetNamespace()).Create(expected)
			if err == nil {
				controller.GetEventRecorder(ctx).Event(kc, corev1.EventTypeNormal, dispatcherDeploymentCreated, "Dispatcher deployment created")
				kc.Status.PropagateDispatcherStatus(&d.Status)
				return d, err
			} else {
				kc.Status.MarkDispatcherFailed(dispatcherDeploymentFailed, "Failed to create the dispatcher deployment: %v", err)
				return d, newDeploymentWarn(err)
			}
		}

		logging.FromContext(ctx).Error("Unable to get the dispatcher deployment", zap.Error(err))
		kc.Status.MarkDispatcherUnknown("DispatcherDeploymentFailed", "Failed to get dispatcher deployment: %v", err)
		return nil, err
	} else if !reflect.DeepEqual(expected.Spec.Template.Spec.Containers[0].Image, d.Spec.Template.Spec.Containers[0].Image) {
		logging.FromContext(ctx).Sugar().Infof("Deployment image is not what we expect it to be, updating Deployment Got: %q Expect: %q", expected.Spec.Template.Spec.Containers[0].Image, d.Spec.Template.Spec.Containers[0].Image)
		d, err := r.KubeClientSet.AppsV1().Deployments(kc.GetNamespace()).Update(expected)
		if err == nil {
			controller.GetEventRecorder(ctx).Event(kc, corev1.EventTypeNormal, dispatcherDeploymentUpdated, "Dispatcher deployment updated")
			kc.Status.PropagateDispatcherStatus(&d.Status)
			return d, nil
		} else {
			kc.Status.MarkServiceFailed("DispatcherDeploymentUpdateFailed", "Failed to update the dispatcher deployment: %v", err)
		}
		return d, newDeploymentWarn(err)
	}

	kc.Status.PropagateDispatcherStatus(&d.Status)
	return d, nil
}

func (r *Reconciler) reconcileServiceAccount(ctx context.Context, kc *v1alpha1.KinesisChannel) (*corev1.ServiceAccount, error) {
	sa, err := r.serviceAccountLister.ServiceAccounts(kc.GetNamespace()).Get(dispatcherName)
	if err != nil {
		if apierrs.IsNotFound(err) {
			expected := resources.MakeServiceAccount(kc.GetNamespace(), dispatcherName)
			sa, err := r.KubeClientSet.CoreV1().ServiceAccounts(kc.GetNamespace()).Create(expected)
			if err == nil {
				controller.GetEventRecorder(ctx).Event(kc, corev1.EventTypeNormal, dispatcherServiceAccountCreated, "Dispatcher service account created")
				return sa, nil
			} else {
				kc.Status.MarkDispatcherFailed("DispatcherDeploymentFailed", "Failed to create the dispatcher service account: %v", err)
				return sa, newServiceAccountWarn(err)
			}
		}

		kc.Status.MarkDispatcherUnknown("DispatcherServiceAccountFailed", "Failed to get dispatcher service account: %v", err)
		return nil, newServiceAccountWarn(err)
	}
	return sa, err
}

func (r *Reconciler) reconcileDispatcherService(ctx context.Context, kc *v1alpha1.KinesisChannel) (*corev1.Service, error) {
	svc, err := r.serviceLister.Services(kc.GetNamespace()).Get(dispatcherName)
	if err != nil {
		if apierrs.IsNotFound(err) {
			expected := resources.MakeDispatcherService(kc.GetNamespace())
			svc, err := r.KubeClientSet.CoreV1().Services(kc.GetNamespace()).Create(expected)

			if err == nil {
				controller.GetEventRecorder(ctx).Event(kc, corev1.EventTypeNormal, dispatcherServiceCreated, "Dispatcher service created")
				kc.Status.MarkServiceTrue()
			} else {
				logging.FromContext(ctx).Error("Unable to create the dispatcher service", zap.Error(err))
				controller.GetEventRecorder(ctx).Eventf(kc, corev1.EventTypeWarning, dispatcherServiceFailed, "Failed to create the dispatcher service: %v", err)
				kc.Status.MarkServiceFailed("DispatcherServiceFailed", "Failed to create the dispatcher service: %v", err)
				return svc, err
			}

			return svc, err
		}

		kc.Status.MarkServiceUnknown("DispatcherServiceFailed", "Failed to get dispatcher service: %v", err)
		return nil, newDispatcherServiceWarn(err)
	}

	kc.Status.MarkServiceTrue()
	return svc, nil
}

func (r *Reconciler) reconcileRoleBinding(ctx context.Context, name string, kc *v1alpha1.KinesisChannel, clusterRoleName string, sa *corev1.ServiceAccount) (*rbacv1.RoleBinding, error) {
	ns := kc.GetNamespace()
	rb, err := r.roleBindingLister.RoleBindings(ns).Get(name)
	if err != nil {
		if apierrs.IsNotFound(err) {
			expected := resources.MakeRoleBinding(ns, name, sa, clusterRoleName)
			rb, err := r.KubeClientSet.RbacV1().RoleBindings(ns).Create(expected)
			if err == nil {
				controller.GetEventRecorder(ctx).Event(kc, corev1.EventTypeNormal, dispatcherRoleBindingCreated, "Dispatcher role binding created")
				return rb, nil
			} else {
				kc.Status.MarkDispatcherFailed("DispatcherDeploymentFailed", "Failed to create the dispatcher role binding: %v", err)
				return rb, newRoleBindingWarn(err)
			}
		}
		kc.Status.MarkDispatcherUnknown("DispatcherRoleBindingFailed", "Failed to get dispatcher role binding: %v", err)
		return nil, newRoleBindingWarn(err)
	}
	return rb, err
}

func (r *Reconciler) reconcileChannelService(ctx context.Context, channel *v1alpha1.KinesisChannel) (*corev1.Service, error) {
	logger := logging.FromContext(ctx)
	// Get the  Service and propagate the status to the Channel in case it does not exist.
	// We don't do anything with the service because it's status contains nothing useful, so just do
	// an existence check. Then below we check the endpoints targeting it.
	// We may change this name later, so we have to ensure we use proper addressable when resolving these.
	expected, err := resources.MakeK8sService(channel, resources.ExternalService(channel.GetNamespace(), dispatcherName))
	if err != nil {
		logging.FromContext(ctx).Error("failed to create the channel service object", zap.Error(err))
		channel.Status.MarkChannelServiceFailed("ChannelServiceFailed", fmt.Sprintf("Channel Service failed: %s", err))
		return nil, err
	}

	svc, err := r.serviceLister.Services(channel.Namespace).Get(resources.MakeChannelServiceName(channel.Name))
	if err != nil {
		if apierrs.IsNotFound(err) {
			svc, err = r.KubeClientSet.CoreV1().Services(channel.Namespace).Create(expected)
			if err != nil {
				logging.FromContext(ctx).Error("failed to create the channel service object", zap.Error(err))
				channel.Status.MarkChannelServiceFailed("ChannelServiceFailed", fmt.Sprintf("Channel Service failed: %s", err))
				return nil, err
			}
			return svc, nil
		}
		logger.Error("Unable to get the channel service", zap.Error(err))
		return nil, err
	} else if !equality.Semantic.DeepEqual(svc.Spec, expected.Spec) {
		svc = svc.DeepCopy()
		svc.Spec = expected.Spec

		svc, err = r.KubeClientSet.CoreV1().Services(channel.Namespace).Update(svc)
		if err != nil {
			logging.FromContext(ctx).Error("Failed to update the channel service", zap.Error(err))
			return nil, err
		}
	}
	// Check to make sure that the KinesisChannel owns this service and if not, complain.
	if !metav1.IsControlledBy(svc, channel) {
		err := fmt.Errorf("kinesischannel: %s/%s does not own Service: %q", channel.Namespace, channel.Name, svc.Name)
		channel.Status.MarkChannelServiceFailed("ChannelServiceFailed", fmt.Sprintf("Channel Service failed: %s", err))
		return nil, err
	}
	return svc, nil
}

func (r *Reconciler) kinesisClient(ctx context.Context, stream, region string, creds *corev1.Secret) (*kinesis.Kinesis, error) {
	if creds == nil {
		return nil, fmt.Errorf("Credentials data is nil")
	}
	keyID, present := creds.Data["aws_access_key_id"]
	if !present {
		return nil, fmt.Errorf("\"aws_access_key_id\" secret key is missing")
	}
	secret, present := creds.Data["aws_secret_access_key"]
	if !present {
		return nil, fmt.Errorf("\"aws_secret_access_key\" secret key is missing")
	}
	logger := logging.FromContext(ctx)
	return kinesisutil.Connect(string(keyID), string(secret), region, logger.Sugar())
}

func (r *Reconciler) setupKinesisStream(ctx context.Context, stream string, kinesisClient *kinesis.Kinesis) error {
	if _, err := kinesisutil.Describe(ctx, kinesisClient, stream); err == nil {
		return nil
	}
	return kinesisutil.Create(ctx, kinesisClient, stream)
}

func (r *Reconciler) removeKinesisStream(ctx context.Context, stream string, kinesisClient *kinesis.Kinesis) error {
	if _, err := kinesisutil.Describe(ctx, kinesisClient, stream); err != nil {
		return nil
	}
	return kinesisutil.Delete(ctx, kinesisClient, stream)
}
