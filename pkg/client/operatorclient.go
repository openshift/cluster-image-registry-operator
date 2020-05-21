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
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type ConfigOperatorClient struct {
	client   imageregistryv1client.ConfigInterface
	informer imageregistryv1informers.ConfigInformer
}

var _ v1helpers.OperatorClient = &ConfigOperatorClient{}

func NewConfigOperatorClient(client imageregistryv1client.ConfigInterface, informer imageregistryv1informers.ConfigInformer) *ConfigOperatorClient {
	return &ConfigOperatorClient{
		client:   client,
		informer: informer,
	}
}

func (c *ConfigOperatorClient) Informer() cache.SharedIndexInformer {
	return c.informer.Informer()
}

func (c *ConfigOperatorClient) GetOperatorState() (spec *operatorv1.OperatorSpec, status *operatorv1.OperatorStatus, resourceVersion string, err error) {
	config, err := c.informer.Lister().Get("cluster")
	if err != nil {
		return nil, nil, "", err
	}

	// TODO(dmage): this should be updated when we add OperatorSpec to the config object
	return &operatorv1.OperatorSpec{}, &config.Status.OperatorStatus, config.ResourceVersion, nil
}

func (c *ConfigOperatorClient) UpdateOperatorSpec(oldResourceVersion string, in *operatorv1.OperatorSpec) (out *operatorv1.OperatorSpec, newResourceVersion string, err error) {
	return nil, "", fmt.Errorf("not implemented")
}

func (c *ConfigOperatorClient) UpdateOperatorStatus(oldResourceVersion string, in *operatorv1.OperatorStatus) (out *operatorv1.OperatorStatus, err error) {
	config, err := c.informer.Lister().Get("cluster")
	if err != nil {
		return nil, err
	}

	if config.ResourceVersion != oldResourceVersion {
		gr := schema.GroupResource{
			Group:    "imageregistry.operator.openshift.io",
			Resource: "configs",
		}
		return nil, errors.NewConflict(gr, config.Name, fmt.Errorf("oldResourceVersion=%s, resourceVersion=%s", oldResourceVersion, config.ResourceVersion))
	}

	config.Status.OperatorStatus = *in

	updatedConfig, err := c.client.UpdateStatus(context.TODO(), config, metav1.UpdateOptions{})
	if err != nil {
		return nil, err
	}

	return &updatedConfig.Status.OperatorStatus, nil
}
