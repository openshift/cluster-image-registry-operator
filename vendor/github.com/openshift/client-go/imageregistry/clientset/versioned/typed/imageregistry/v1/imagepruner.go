// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	"context"
	json "encoding/json"
	"fmt"
	"time"

	v1 "github.com/openshift/api/imageregistry/v1"
	imageregistryv1 "github.com/openshift/client-go/imageregistry/applyconfigurations/imageregistry/v1"
	scheme "github.com/openshift/client-go/imageregistry/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ImagePrunersGetter has a method to return a ImagePrunerInterface.
// A group's client should implement this interface.
type ImagePrunersGetter interface {
	ImagePruners() ImagePrunerInterface
}

// ImagePrunerInterface has methods to work with ImagePruner resources.
type ImagePrunerInterface interface {
	Create(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.CreateOptions) (*v1.ImagePruner, error)
	Update(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.UpdateOptions) (*v1.ImagePruner, error)
	UpdateStatus(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.UpdateOptions) (*v1.ImagePruner, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.ImagePruner, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.ImagePrunerList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ImagePruner, err error)
	Apply(ctx context.Context, imagePruner *imageregistryv1.ImagePrunerApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImagePruner, err error)
	ApplyStatus(ctx context.Context, imagePruner *imageregistryv1.ImagePrunerApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImagePruner, err error)
	ImagePrunerExpansion
}

// imagePruners implements ImagePrunerInterface
type imagePruners struct {
	client rest.Interface
}

// newImagePruners returns a ImagePruners
func newImagePruners(c *ImageregistryV1Client) *imagePruners {
	return &imagePruners{
		client: c.RESTClient(),
	}
}

// Get takes name of the imagePruner, and returns the corresponding imagePruner object, and an error if there is any.
func (c *imagePruners) Get(ctx context.Context, name string, options metav1.GetOptions) (result *v1.ImagePruner, err error) {
	result = &v1.ImagePruner{}
	err = c.client.Get().
		Resource("imagepruners").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ImagePruners that match those selectors.
func (c *imagePruners) List(ctx context.Context, opts metav1.ListOptions) (result *v1.ImagePrunerList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1.ImagePrunerList{}
	err = c.client.Get().
		Resource("imagepruners").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested imagePruners.
func (c *imagePruners) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Resource("imagepruners").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a imagePruner and creates it.  Returns the server's representation of the imagePruner, and an error, if there is any.
func (c *imagePruners) Create(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.CreateOptions) (result *v1.ImagePruner, err error) {
	result = &v1.ImagePruner{}
	err = c.client.Post().
		Resource("imagepruners").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imagePruner).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a imagePruner and updates it. Returns the server's representation of the imagePruner, and an error, if there is any.
func (c *imagePruners) Update(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.UpdateOptions) (result *v1.ImagePruner, err error) {
	result = &v1.ImagePruner{}
	err = c.client.Put().
		Resource("imagepruners").
		Name(imagePruner.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imagePruner).
		Do(ctx).
		Into(result)
	return
}

// UpdateStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating UpdateStatus().
func (c *imagePruners) UpdateStatus(ctx context.Context, imagePruner *v1.ImagePruner, opts metav1.UpdateOptions) (result *v1.ImagePruner, err error) {
	result = &v1.ImagePruner{}
	err = c.client.Put().
		Resource("imagepruners").
		Name(imagePruner.Name).
		SubResource("status").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(imagePruner).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the imagePruner and deletes it. Returns an error if one occurs.
func (c *imagePruners) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete().
		Resource("imagepruners").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *imagePruners) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Resource("imagepruners").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched imagePruner.
func (c *imagePruners) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.ImagePruner, err error) {
	result = &v1.ImagePruner{}
	err = c.client.Patch(pt).
		Resource("imagepruners").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}

// Apply takes the given apply declarative configuration, applies it and returns the applied imagePruner.
func (c *imagePruners) Apply(ctx context.Context, imagePruner *imageregistryv1.ImagePrunerApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImagePruner, err error) {
	if imagePruner == nil {
		return nil, fmt.Errorf("imagePruner provided to Apply must not be nil")
	}
	patchOpts := opts.ToPatchOptions()
	data, err := json.Marshal(imagePruner)
	if err != nil {
		return nil, err
	}
	name := imagePruner.Name
	if name == nil {
		return nil, fmt.Errorf("imagePruner.Name must be provided to Apply")
	}
	result = &v1.ImagePruner{}
	err = c.client.Patch(types.ApplyPatchType).
		Resource("imagepruners").
		Name(*name).
		VersionedParams(&patchOpts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}

// ApplyStatus was generated because the type contains a Status member.
// Add a +genclient:noStatus comment above the type to avoid generating ApplyStatus().
func (c *imagePruners) ApplyStatus(ctx context.Context, imagePruner *imageregistryv1.ImagePrunerApplyConfiguration, opts metav1.ApplyOptions) (result *v1.ImagePruner, err error) {
	if imagePruner == nil {
		return nil, fmt.Errorf("imagePruner provided to Apply must not be nil")
	}
	patchOpts := opts.ToPatchOptions()
	data, err := json.Marshal(imagePruner)
	if err != nil {
		return nil, err
	}

	name := imagePruner.Name
	if name == nil {
		return nil, fmt.Errorf("imagePruner.Name must be provided to Apply")
	}

	result = &v1.ImagePruner{}
	err = c.client.Patch(types.ApplyPatchType).
		Resource("imagepruners").
		Name(*name).
		SubResource("status").
		VersionedParams(&patchOpts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}
