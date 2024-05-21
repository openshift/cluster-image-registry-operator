package client

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/cache"

	operatorv1 "github.com/openshift/api/operator/v1"
	imageregistryv1client "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"
	imageregistryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type ConfigOperatorClient struct {
	client   imageregistryv1client.ConfigInterface
	informer cache.SharedIndexInformer
	lister   imageregistryv1listers.ConfigLister
}

// GetOperatorStateWithQuorum implements v1helpers.OperatorClient.
func (c *ConfigOperatorClient) GetOperatorStateWithQuorum(ctx context.Context) (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	config, err := c.lister.Get("cluster")
	if err != nil {
		return nil, nil, "", err
	}
	config = config.DeepCopy()

	return &config.Spec.OperatorSpec, &config.Status.OperatorStatus, config.ResourceVersion, nil
}

var _ v1helpers.OperatorClient = &ConfigOperatorClient{}

func NewConfigOperatorClient(client imageregistryv1client.ConfigInterface, informer imageregistryv1informers.ConfigInformer) *ConfigOperatorClient {
	return &ConfigOperatorClient{
		client:   client,
		informer: informer.Informer(),
		lister:   informer.Lister(),
	}
}

func (c *ConfigOperatorClient) Informer() cache.SharedIndexInformer {
	return c.informer
}

func (c *ConfigOperatorClient) GetObjectMeta() (meta *metav1.ObjectMeta, err error) {
	config, err := c.lister.Get("cluster")
	if err != nil {
		return nil, err
	}
	config = config.DeepCopy()

	return &config.ObjectMeta, nil
}

func (c *ConfigOperatorClient) GetOperatorState() (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	config, err := c.lister.Get("cluster")
	if err != nil {
		return nil, nil, "", err
	}
	config = config.DeepCopy()

	return &config.Spec.OperatorSpec, &config.Status.OperatorStatus, config.ResourceVersion, nil
}

func (c *ConfigOperatorClient) UpdateOperatorSpec(ctx context.Context, oldResourceVersion string, in *operatorv1.OperatorSpec) (out *operatorv1.OperatorSpec, newResourceVersion string, err error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (c *ConfigOperatorClient) UpdateOperatorStatus(ctx context.Context, oldResourceVersion string, in *operatorv1.OperatorStatus) (out *operatorv1.OperatorStatus, err error) {
	config, err := c.lister.Get("cluster")
	if err != nil {
		return nil, err
	}
	config = config.DeepCopy()

	if config.ResourceVersion != oldResourceVersion {
		gr := schema.GroupResource{
			Group:    "imageregistry.operator.openshift.io",
			Resource: "configs",
		}
		return nil, errors.NewConflict(gr, config.Name, fmt.Errorf("oldResourceVersion=%s, resourceVersion=%s", oldResourceVersion, config.ResourceVersion))
	}

	config.Status.OperatorStatus = *in

	updatedConfig, err := c.client.UpdateStatus(ctx, config, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return &updatedConfig.Status.OperatorStatus, nil
}
