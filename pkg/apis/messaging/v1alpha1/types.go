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

package v1alpha1

import (
	eventingduck "github.com/knative/eventing/pkg/apis/duck/v1alpha1"
	duckv1alpha1 "github.com/knative/pkg/apis/duck/v1alpha1"
	duckv1beta1 "github.com/knative/pkg/apis/duck/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KinesisChannel is a specification for a KinesisChannel resource
type KinesisChannel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KinesisChannelSpec   `json:"spec"`
	Status KinesisChannelStatus `json:"status,omitempty"`
}

// KinesisChannelSpec is the spec for a KinesisChannel resource
type KinesisChannelSpec struct {
	Subscribable  *eventingduck.Subscribable `json:"subscribable,omitempty"`
	StreamName    string                     `json:"stream_name"`
	AccountRegion string                     `json:"account_region"`
	AccountCreds  string                     `json:"account_creds"`
}

// KinesisChannelStatus is the status for a KinesisChannel resource
type KinesisChannelStatus struct {
	// inherits duck/v1beta1 Status, which currently provides:
	// * ObservedGeneration - the 'Generation' of the Service that was last processed by the controller.
	// * Conditions - the latest available observations of a resource's current state.
	duckv1beta1.Status `json:",inline"`

	// KinesisChannel is Addressable. It currently exposes the endpoint as a
	// fully-qualified DNS name which will distribute traffic over the
	// provided targets from inside the cluster.
	//
	// It generally has the form {channel}.{namespace}.svc.{cluster domain name}
	duckv1alpha1.AddressStatus `json:",inline"`

	// Subscribers is populated with the statuses of each of the Channelable's subscribers.
	eventingduck.SubscribableTypeStatus `json:",inline"`

	StreamReady bool `json:",omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// KinesisChannelList is a list of KinesisChannel resources
type KinesisChannelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []KinesisChannel `json:"items"`
}

// GetGroupVersionKind returns GroupVersionKind for KinesisChannels
func (c *KinesisChannel) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("KinesisChannel")
}
