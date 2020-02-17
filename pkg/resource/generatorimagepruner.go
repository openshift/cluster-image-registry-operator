package resource

import (
	"fmt"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

func NewImagePrunerGenerator(kubeconfig *rest.Config, clients *client.Clients, listers *client.ImagePrunerControllerListers, params *parameters.Globals) *ImagePrunerGenerator {
	return &ImagePrunerGenerator{
		kubeconfig: kubeconfig,
		listers:    listers,
		clients:    clients,
		params:     params,
	}
}

type ImagePrunerGenerator struct {
	kubeconfig *rest.Config
	listers    *client.ImagePrunerControllerListers
	clients    *client.Clients
	params     *parameters.Globals
}

func (g *ImagePrunerGenerator) List(cr *imageregistryv1.ImagePruner) ([]Mutator, error) {
	var mutators []Mutator
	mutators = append(mutators, newGeneratorPrunerClusterRoleBinding(g.listers.ClusterRoleBindings, g.clients.RBAC, g.params))
	mutators = append(mutators, newGeneratorPrunerServiceAccount(g.listers.ServiceAccounts, g.clients.Core, g.params))
	mutators = append(mutators, newGeneratorPrunerCronJob(g.listers.CronJobs, g.clients.Batch, g.listers.ImagePrunerConfigs, g.listers.RegistryConfigs, g.params))

	return mutators, nil
}

func (g *ImagePrunerGenerator) Apply(pcr *imageregistryv1.ImagePruner) error {
	generators, err := g.List(pcr)
	if err != nil {
		return fmt.Errorf("unable to get generators: %s", err)
	}

	for _, gen := range generators {
		err = ApplyMutator(gen)
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	return nil
}

func (g *ImagePrunerGenerator) Remove(cr *imageregistryv1.ImagePruner) error {
	generators, err := g.List(cr)
	if err != nil {
		return fmt.Errorf("unable to get generators: %s", err)
	}

	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground
	opts := &metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}
	for _, gen := range generators {
		if !gen.Owned() {
			continue
		}
		if err := gen.Delete(opts); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete object %s: %s", Name(gen), err)
		}
		klog.Infof("object %s deleted", Name(gen))
	}

	return nil
}
