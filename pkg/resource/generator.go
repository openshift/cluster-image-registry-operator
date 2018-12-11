package resource

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func NewGenerator(kubeconfig *rest.Config, listers *client.Listers, params *parameters.Globals) *Generator {
	return &Generator{
		kubeconfig: kubeconfig,
		listers:    listers,
		params:     params,
	}
}

type Generator struct {
	kubeconfig *rest.Config
	listers    *client.Listers
	params     *parameters.Globals
}

func (g *Generator) listRoutes(routeClient routeset.RouteV1Interface, cr *regopapi.ImageRegistry) []Mutator {
	var mutators []Mutator
	mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, routeClient, g.params, cr, regopapi.ImageRegistryConfigRoute{
		Name: cr.Name + "-default-route",
	}))
	for _, route := range cr.Spec.Routes {
		mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, routeClient, g.params, cr, route))
	}
	return mutators
}

func (g *Generator) list(cr *regopapi.ImageRegistry) ([]Mutator, error) {
	coreClient, err := coreset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	appsClient, err := appsset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	rbacClient, err := rbacset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	routeClient, err := routeset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	configClient, err := configset.NewForConfig(g.kubeconfig)
	if err != nil {
		return nil, err
	}

	var mutators []Mutator
	mutators = append(mutators, newGeneratorClusterRole(g.listers.ClusterRoles, rbacClient, cr))
	mutators = append(mutators, newGeneratorClusterRoleBinding(g.listers.ClusterRoleBindings, rbacClient, g.params, cr))
	mutators = append(mutators, newGeneratorServiceAccount(g.listers.ServiceAccounts, coreClient, g.params, cr))
	mutators = append(mutators, newGeneratorCAConfig(g.listers.ConfigMaps, g.listers.OpenShiftConfig, coreClient, g.params, cr))
	mutators = append(mutators, newGeneratorSecret(g.listers.Secrets, coreClient, g.params, cr))
	mutators = append(mutators, newGeneratorImageConfig(g.listers.ImageConfigs, configClient, g.params, cr))
	mutators = append(mutators, newGeneratorService(g.listers.Services, coreClient, g.params, cr))
	mutators = append(mutators, newGeneratorDeployment(g.listers.Deployments, g.listers.ConfigMaps, g.listers.Secrets, coreClient, appsClient, g.params, cr))
	mutators = append(mutators, g.listRoutes(routeClient, cr)...)
	return mutators, nil
}

func (g *Generator) removeObsoleteRoutes(cr *regopapi.ImageRegistry, modified *bool) error {
	routeClient, err := routeset.NewForConfig(g.kubeconfig)
	if err != nil {
		return err
	}

	routes, err := g.listers.Routes.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list routes: %s", err)
	}

	routesGenerators := g.listRoutes(routeClient, cr)
	knownNames := map[string]struct{}{}
	for _, gen := range routesGenerators {
		knownNames[gen.GetName()] = struct{}{}
	}

	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground
	opts := &metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}
	for _, route := range routes {
		if !metaapi.IsControlledBy(route, cr) {
			continue
		}
		if _, found := knownNames[route.Name]; found {
			continue
		}
		err = routeClient.Routes(g.params.Deployment.Namespace).Delete(route.Name, opts)
		if err != nil {
			return err
		}
		*modified = true
	}
	return nil
}

func (g *Generator) Apply(cr *regopapi.ImageRegistry, modified *bool) error {
	generators, err := g.list(cr)
	if err != nil {
		return fmt.Errorf("unable to get generators: %s", err)
	}

	for _, gen := range generators {
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			o, err := gen.Get()
			if err != nil {
				if !errors.IsNotFound(err) {
					return fmt.Errorf("failed to get object %s: %s", Name(gen), err)
				}

				err = gen.Create()
				if err != nil {
					return fmt.Errorf("failed to create object %s: %s", Name(gen), err)
				}
				glog.Infof("object %s created", Name(gen))
				*modified = true
				return nil
			}

			updated, err := gen.Update(o.DeepCopyObject())
			if err != nil {
				return fmt.Errorf("failed to update object %s: %s", Name(gen), err)
			}
			if updated {
				glog.Infof("object %s updated", Name(gen))
				*modified = true
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	err = g.removeObsoleteRoutes(cr, modified)
	if err != nil {
		return fmt.Errorf("unable to remove obsolete routes: %s", err)
	}

	return nil
}

func (g *Generator) Remove(cr *regopapi.ImageRegistry, modified *bool) error {
	generators, err := g.list(cr)
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
		if err := gen.Delete(opts); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return fmt.Errorf("failed to delete object %s: %s", Name(gen), err)
		}
		glog.Infof("object %s deleted", Name(gen))
		*modified = true
	}

	return nil
}
