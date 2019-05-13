// Copyright 2019 TriggerMesh, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package channel

import (
	"context"

	eventingv1alpha1 "github.com/knative/eventing/pkg/apis/eventing/v1alpha1"
	"github.com/knative/eventing/pkg/logging"
	util "github.com/knative/eventing/pkg/provisioners"
	"github.com/knative/eventing/pkg/reconciler/names"
	ccpcontroller "github.com/triggermesh/aws-kinesis-provisioner/pkg/controller/clusterchannelprovisioner"
	"github.com/triggermesh/aws-kinesis-provisioner/pkg/kinesis"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	finalizerName = controllerAgentName
)

type persistence int

const (
	persistStatus persistence = iota
	noNeedToPersist

	// Name of the corev1.Events emitted from the reconciliation process
	channelReconciled          = "ChannelReconciled"
	channelUpdateStatusFailed  = "ChannelUpdateStatusFailed"
	channelReadStatusFailed    = "ChannelReadStatusFailed"
	virtualServiceCreateFailed = "VirtualServiceCreateFailed"
	k8sServiceCreateFailed     = "K8sServiceCreateFailed"
	streamCreateFailed         = "StreamCreateFailed"
)

// reconciler reconciles ibm-mq Channels by creating the K8s Service and Istio VirtualService
// allowing other processes to send data to them.
type reconciler struct {
	client   client.Client
	recorder record.EventRecorder
	logger   *zap.Logger
	qClient  *kinesis.Client
}

// Verify the struct implements reconcile.Reconciler
var _ reconcile.Reconciler = &reconciler{}

func (r *reconciler) InjectClient(c client.Client) error {
	r.client = c
	return nil
}

func (r *reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.TODO()
	ctx = logging.WithLogger(ctx, r.logger.With(zap.Any("request", request)))

	c := &eventingv1alpha1.Channel{}
	err := r.client.Get(ctx, request.NamespacedName, c)

	// The Channel may have been deleted since it was added to the workqueue. If so, there is
	// nothing to be done.
	if errors.IsNotFound(err) {
		logging.FromContext(ctx).Info("Could not find Channel", zap.Error(err))
		return reconcile.Result{}, nil
	}

	// Any other error should be retried in another reconciliation.
	if err != nil {
		logging.FromContext(ctx).Error("Unable to Get Channel", zap.Error(err))
		return reconcile.Result{}, err
	}

	// Does this Controller control this Channel?
	if !r.shouldReconcile(c) {
		logging.FromContext(ctx).Info("Not reconciling Channel, it is not controlled by this Controller", zap.Any("ref", c.Spec))
		return reconcile.Result{}, nil
	}
	logging.FromContext(ctx).Info("Reconciling Channel")

	// Modify a copy, not the original.
	c = c.DeepCopy()

	ctx = logging.WithLogger(ctx, logging.FromContext(ctx).With(zap.Any("channel", c)))
	requeue, reconcileErr := r.reconcile(ctx, c)
	if reconcileErr != nil {
		logging.FromContext(ctx).Info("Error reconciling Channel", zap.Error(reconcileErr))
		// Note that we do not return the error here, because we want to update the Status
		// regardless of the error.
	} else {
		logging.FromContext(ctx).Info("Channel reconciled")
		r.recorder.Eventf(c, v1.EventTypeNormal, channelReconciled, "Channel reconciled: %q", c.Name)
	}

	if err = util.UpdateChannel(ctx, r.client, c); err != nil {
		logging.FromContext(ctx).Info("Error updating Channel Status", zap.Error(err))
		r.recorder.Eventf(c, v1.EventTypeWarning, channelUpdateStatusFailed, "Failed to update Channel's status: %v", err)
		return reconcile.Result{}, err
	}

	return reconcile.Result{
		Requeue: requeue,
	}, reconcileErr
}

// shouldReconcile determines if this Controller should control (and therefore reconcile) a given
// Channel. This Controller only handles idm-mq channels.
func (r *reconciler) shouldReconcile(c *eventingv1alpha1.Channel) bool {
	if c.Spec.Provisioner != nil {
		return ccpcontroller.IsControlled(c.Spec.Provisioner)
	}
	return false
}

// reconcile reconciles this Channel so that the real world matches the intended state. The returned
// boolean indicates if this Channel should be immediately requeued for another reconcile loop. The
// returned error indicates an error during reconciliation.
func (r *reconciler) reconcile(ctx context.Context, c *eventingv1alpha1.Channel) (bool, error) {
	c.Status.InitializeConditions()
	stream, err := r.qClient.DescribeKinesisStream(c.Name)
	if err != nil {
		logging.FromContext(ctx).Info("Failed to describe kinesis stream", zap.Error(err))
		if err := r.qClient.CreateKinesisStream(c.Name); err != nil {
			r.recorder.Eventf(c, v1.EventTypeWarning, streamCreateFailed, "Failed to create queue for the Channel: %v", err)
			return false, err
		}
	}

	if *stream.StreamStatus == "DELETING" {
		util.RemoveFinalizer(c, finalizerName)
		return false, nil
	}

	if addFinalizerResult := util.AddFinalizer(c, finalizerName); addFinalizerResult == util.FinalizerAdded {
		return true, nil
	}

	svc, err := r.createK8sService(ctx, c)
	if err != nil {
		r.recorder.Eventf(c, v1.EventTypeWarning, k8sServiceCreateFailed, "Failed to reconcile Channel's K8s Service: %v", err)
		return false, err
	}

	err = r.createVirtualService(ctx, c, svc)
	if err != nil {
		r.recorder.Eventf(c, v1.EventTypeWarning, virtualServiceCreateFailed, "Failed to reconcile Virtual Service for the Channel: %v", err)
		return false, err
	}

	c.Status.MarkProvisioned()
	return false, nil
}

func (r *reconciler) createK8sService(ctx context.Context, c *eventingv1alpha1.Channel) (*v1.Service, error) {
	svc, err := util.CreateK8sService(ctx, r.client, c)
	if err != nil {
		logging.FromContext(ctx).Info("Error creating the Channel's K8s Service", zap.Error(err))
		return nil, err
	}

	c.Status.SetAddress(names.ServiceHostName(svc.Name, svc.Namespace))
	return svc, nil
}

func (r *reconciler) createVirtualService(ctx context.Context, c *eventingv1alpha1.Channel, svc *v1.Service) error {
	_, err := util.CreateVirtualService(ctx, r.client, c, svc)
	if err != nil {
		logging.FromContext(ctx).Info("Error creating the Virtual Service for the Channel", zap.Error(err))
		return err
	}
	return nil
}
