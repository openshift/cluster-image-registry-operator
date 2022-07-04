// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	"context"

	configv1 "github.com/openshift/api/config/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeNodes implements NodeInterface
type FakeNodes struct {
	Fake *FakeConfigV1
}

var nodesResource = schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "nodes"}

var nodesKind = schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "Node"}

// Get takes name of the node, and returns the corresponding node object, and an error if there is any.
func (c *FakeNodes) Get(ctx context.Context, name string, options v1.GetOptions) (result *configv1.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootGetAction(nodesResource, name), &configv1.Node{})
	if obj == nil {
		return nil, err
	}
	return obj.(*configv1.Node), err
}

// List takes label and field selectors, and returns the list of Nodes that match those selectors.
func (c *FakeNodes) List(ctx context.Context, opts v1.ListOptions) (result *configv1.NodeList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootListAction(nodesResource, nodesKind, opts), &configv1.NodeList{})
	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &configv1.NodeList{ListMeta: obj.(*configv1.NodeList).ListMeta}
	for _, item := range obj.(*configv1.NodeList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested nodes.
func (c *FakeNodes) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewRootWatchAction(nodesResource, opts))
}

// Create takes the representation of a node and creates it.  Returns the server's representation of the node, and an error, if there is any.
func (c *FakeNodes) Create(ctx context.Context, node *configv1.Node, opts v1.CreateOptions) (result *configv1.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootCreateAction(nodesResource, node), &configv1.Node{})
	if obj == nil {
		return nil, err
	}
	return obj.(*configv1.Node), err
}

// Update takes the representation of a node and updates it. Returns the server's representation of the node, and an error, if there is any.
func (c *FakeNodes) Update(ctx context.Context, node *configv1.Node, opts v1.UpdateOptions) (result *configv1.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateAction(nodesResource, node), &configv1.Node{})
	if obj == nil {
		return nil, err
	}
	return obj.(*configv1.Node), err
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *FakeNodes) UpdateStatus(ctx context.Context, node *configv1.Node, opts v1.UpdateOptions) (*configv1.Node, error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootUpdateSubresourceAction(nodesResource, "status", node), &configv1.Node{})
	if obj == nil {
		return nil, err
	}
	return obj.(*configv1.Node), err
}

// Delete takes name of the node and deletes it. Returns an error if one occurs.
func (c *FakeNodes) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewRootDeleteActionWithOptions(nodesResource, name, opts), &configv1.Node{})
	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNodes) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewRootDeleteCollectionAction(nodesResource, listOpts)

	_, err := c.Fake.Invokes(action, &configv1.NodeList{})
	return err
}

// Patch applies the patch and returns the patched node.
func (c *FakeNodes) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *configv1.Node, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewRootPatchSubresourceAction(nodesResource, name, pt, data, subresources...), &configv1.Node{})
	if obj == nil {
		return nil, err
	}
	return obj.(*configv1.Node), err
}
