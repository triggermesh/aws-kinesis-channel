/*
Copyright The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/triggermesh/aws-kinesis-provisioner/pkg/apis/messaging/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeKinesisChannels implements KinesisChannelInterface
type FakeKinesisChannels struct {
	Fake *FakeMessagingV1alpha1
	ns   string
}

var kinesischannelsResource = schema.GroupVersionResource{Group: "messaging.triggermesh.dev", Version: "v1alpha1", Resource: "kinesischannels"}

var kinesischannelsKind = schema.GroupVersionKind{Group: "messaging.triggermesh.dev", Version: "v1alpha1", Kind: "KinesisChannel"}

// Get takes name of the kinesisChannel, and returns the corresponding kinesisChannel object, and an error if there is any.
func (c *FakeKinesisChannels) Get(name string, options v1.GetOptions) (result *v1alpha1.KinesisChannel, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(kinesischannelsResource, c.ns, name), &v1alpha1.KinesisChannel{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.KinesisChannel), err
}

// List takes label and field selectors, and returns the list of KinesisChannels that match those selectors.
func (c *FakeKinesisChannels) List(opts v1.ListOptions) (result *v1alpha1.KinesisChannelList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(kinesischannelsResource, kinesischannelsKind, c.ns, opts), &v1alpha1.KinesisChannelList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.KinesisChannelList{ListMeta: obj.(*v1alpha1.KinesisChannelList).ListMeta}
	for _, item := range obj.(*v1alpha1.KinesisChannelList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested kinesisChannels.
func (c *FakeKinesisChannels) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(kinesischannelsResource, c.ns, opts))

}

// Create takes the representation of a kinesisChannel and creates it.  Returns the server's representation of the kinesisChannel, and an error, if there is any.
func (c *FakeKinesisChannels) Create(kinesisChannel *v1alpha1.KinesisChannel) (result *v1alpha1.KinesisChannel, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(kinesischannelsResource, c.ns, kinesisChannel), &v1alpha1.KinesisChannel{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.KinesisChannel), err
}

// Update takes the representation of a kinesisChannel and updates it. Returns the server's representation of the kinesisChannel, and an error, if there is any.
func (c *FakeKinesisChannels) Update(kinesisChannel *v1alpha1.KinesisChannel) (result *v1alpha1.KinesisChannel, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(kinesischannelsResource, c.ns, kinesisChannel), &v1alpha1.KinesisChannel{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.KinesisChannel), err
}

// Delete takes name of the kinesisChannel and deletes it. Returns an error if one occurs.
func (c *FakeKinesisChannels) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(kinesischannelsResource, c.ns, name), &v1alpha1.KinesisChannel{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeKinesisChannels) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(kinesischannelsResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &v1alpha1.KinesisChannelList{})
	return err
}

// Patch applies the patch and returns the patched kinesisChannel.
func (c *FakeKinesisChannels) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.KinesisChannel, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(kinesischannelsResource, c.ns, name, pt, data, subresources...), &v1alpha1.KinesisChannel{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.KinesisChannel), err
}
