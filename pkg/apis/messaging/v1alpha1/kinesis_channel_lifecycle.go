/*
Copyright 2019 The Knative Authors

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

package v1alpha1

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	v1 "knative.dev/pkg/apis/duck/v1"
)

var kc = apis.NewLivingConditionSet(
	KinesisChannelConditionDispatcherReady,
	KinesisChannelConditionServiceReady,
	KinesisChannelConditionEndpointsReady,
	KinesisChannelConditionStreamReady,
	KinesisChannelConditionAddressable,
	KinesisChannelConditionChannelServiceReady)

const (
	// KinesisChannelConditionReady has status True when all subconditions below have been set to True.
	KinesisChannelConditionReady = apis.ConditionReady

	// KinesisChannelConditionDispatcherReady has status True when a Dispatcher deployment is ready
	// Keyed off appsv1.DeploymentAvailable, which means minimum available replicas required are up
	// and running for at least minReadySeconds.
	KinesisChannelConditionDispatcherReady apis.ConditionType = "DispatcherReady"

	// KinesisChannelConditionServiceReady has status True when a k8s Service is ready. This
	// basically just means it exists because there's no meaningful status in Service. See Endpoints
	// below.
	KinesisChannelConditionServiceReady apis.ConditionType = "ServiceReady"

	// KinesisChannelConditionEndpointsReady has status True when a k8s Service Endpoints are backed
	// by at least one endpoint.
	KinesisChannelConditionEndpointsReady apis.ConditionType = "EndpointsReady"

	KinesisChannelConditionStreamReady apis.ConditionType = "StreamReady"

	// KinesisChannelConditionAddressable has status true when this KinesisChannel meets
	// the Addressable contract and has a non-empty hostname.
	KinesisChannelConditionAddressable apis.ConditionType = "Addressable"

	// KinesisChannelConditionChannelServiceReady has status True when a k8s Service representing the channel is ready.
	// Because this uses ExternalName, there are no endpoints to check.
	KinesisChannelConditionChannelServiceReady apis.ConditionType = "ChannelServiceReady"
)

// GetConditionSet retrieves the condition set for this resource. Implements the KRShaped interface.
func (k *KinesisChannel) GetConditionSet() apis.ConditionSet {
	return kc
}

// GetCondition returns the condition currently associated with the given type, or nil.
func (cs *KinesisChannelStatus) GetCondition(t apis.ConditionType) *apis.Condition {
	return kc.Manage(cs).GetCondition(t)
}

// IsReady returns true if the resource is ready overall.
func (cs *KinesisChannelStatus) IsReady() bool {
	return kc.Manage(cs).IsHappy()
}

// GetStatus retrieves the status of the KinesisChannel. Implements the KRShaped interface.
func (k *KinesisChannel) GetStatus() *duckv1.Status {
	return &k.Status.Status
}

// InitializeConditions sets relevant unset conditions to Unknown state.
func (cs *KinesisChannelStatus) InitializeConditions() {
	kc.Manage(cs).InitializeConditions()
}

// SetAddress sets the address (as part of Addressable contract) and marks the correct condition.
func (cs *KinesisChannelStatus) SetAddress(url *apis.URL) {
	if cs.Address == nil {
		cs.Address = &v1.Addressable{}
	}
	if url != nil {
		cs.Address.URL = url
		kc.Manage(cs).MarkTrue(KinesisChannelConditionAddressable)
	} else {
		cs.Address.URL = nil
		kc.Manage(cs).MarkFalse(KinesisChannelConditionAddressable, "EmptyHostname", "hostname is the empty string")
	}
}

func (cs *KinesisChannelStatus) MarkDispatcherFailed(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkFalse(KinesisChannelConditionDispatcherReady, reason, messageFormat, messageA...)
}

// TODO: Unify this with the ones from Eventing. Say: Broker, Trigger.
func (cs *KinesisChannelStatus) PropagateDispatcherStatus(ds *appsv1.DeploymentStatus) {
	for _, cond := range ds.Conditions {
		if cond.Type == appsv1.DeploymentAvailable {
			if cond.Status != corev1.ConditionTrue {
				cs.MarkDispatcherFailed("DispatcherNotReady", "Dispatcher Deployment is not ready: %s : %s", cond.Reason, cond.Message)
			} else {
				kc.Manage(cs).MarkTrue(KinesisChannelConditionDispatcherReady)
			}
		}
	}
}

func (cs *KinesisChannelStatus) MarkDispatcherUnknown(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkUnknown(KinesisChannelConditionDispatcherReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkServiceFailed(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkFalse(KinesisChannelConditionServiceReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkServiceTrue() {
	kc.Manage(cs).MarkTrue(KinesisChannelConditionServiceReady)
}

func (cs *KinesisChannelStatus) MarkServiceUnknown(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkUnknown(KinesisChannelConditionServiceReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkChannelServiceFailed(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkFalse(KinesisChannelConditionChannelServiceReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkChannelServiceTrue() {
	kc.Manage(cs).MarkTrue(KinesisChannelConditionChannelServiceReady)
}

func (cs *KinesisChannelStatus) MarkEndpointsFailed(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkFalse(KinesisChannelConditionEndpointsReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkEndpointsTrue() {
	kc.Manage(cs).MarkTrue(KinesisChannelConditionEndpointsReady)
}

func (cs *KinesisChannelStatus) MarkStreamUnknown(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkUnknown(KinesisChannelConditionStreamReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkStreamFailed(reason, messageFormat string, messageA ...interface{}) {
	kc.Manage(cs).MarkFalse(KinesisChannelConditionStreamReady, reason, messageFormat, messageA...)
}

func (cs *KinesisChannelStatus) MarkStreamTrue() {
	kc.Manage(cs).MarkTrue(KinesisChannelConditionStreamReady)
}
