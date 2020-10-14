package resource

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func NewImagePrunerGenerator(clients *client.Clients, listers *client.ImagePrunerControllerListers) *ImagePrunerGenerator {
	return &ImagePrunerGenerator{
		listers: listers,
		clients: clients,
	}
}

type ImagePrunerGenerator struct {
	listers *client.ImagePrunerControllerListers
	clients *client.Clients
}

func (g *ImagePrunerGenerator) List(cr *imageregistryv1.ImagePruner) ([]Mutator, error) {
	var mutators []Mutator
	mutators = append(mutators, newGeneratorPrunerClusterRoleBinding(g.listers.ClusterRoleBindings, g.clients.RBAC))
	mutators = append(mutators, newGeneratorPrunerServiceAccount(g.listers.ServiceAccounts, g.clients.Core))
	mutators = append(mutators, newGeneratorServiceCA(g.listers.ConfigMaps, g.clients.Core))
	mutators = append(mutators, newGeneratorPrunerCronJob(g.listers.CronJobs, g.clients.Batch, g.listers.ImagePrunerConfigs, g.listers.RegistryConfigs))

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
	opts := metaapi.DeleteOptions{
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
