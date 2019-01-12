package resource

import (
	"fmt"
	"github.com/golang/glog"

	routeset "github.com/openshift/client-go/route/clientset/versioned/typed/route/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	rbacset "k8s.io/client-go/kubernetes/typed/rbac/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
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

func (g *Generator) listRoutes(routeClient routeset.RouteV1Interface, cr *imageregistryv1.Config) []Mutator {
	var mutators []Mutator
	mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, routeClient, g.params, cr, imageregistryv1.ImageRegistryConfigRoute{
		Name: cr.Name + "-default-route",
	}))
	for _, route := range cr.Spec.Routes {
		mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, routeClient, g.params, cr, route))
	}
	return mutators
}

func (g *Generator) list(cr *imageregistryv1.Config) ([]Mutator, error) {
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
	mutators = append(mutators, newGeneratorNodeCADaemonSet(g.listers.DaemonSets, appsClient, g.params, cr))
	mutators = append(mutators, newGeneratorService(g.listers.Services, coreClient, g.params, cr))
	mutators = append(mutators, newGeneratorDeployment(g.listers.Deployments, g.listers.ConfigMaps, g.listers.Secrets, coreClient, appsClient, g.params, cr))
	mutators = append(mutators, g.listRoutes(routeClient, cr)...)
	return mutators, nil
}

// syncStorage checks:
// 1.)  to make sure that an existing storage medium still exists and we can access it
// 2.)  to see if the storage medium name changed and we need to:
//      a.) check to make sure that we can access the storage or
//      b.) see if we need to try to create the new storage
func (g *Generator) syncStorage(cr *imageregistryv1.Config, modified *bool) error {
	var runCreate bool
	// Create a driver with the current configuration
	driver, err := storage.NewDriver(cr.ObjectMeta.Name, cr.ObjectMeta.Namespace, &cr.Spec.Storage)
	if err != nil {
		return err
	}

	if driver.StorageChanged(cr, modified) {
		runCreate = true
	} else {
		exists, err := driver.StorageExists(cr, modified)
		if err != nil {
			return err
		}
		if !exists {
			runCreate = true
		}
	}

	if runCreate {
		if err := driver.CreateStorage(cr, modified); err != nil {
			return err
		}
	}
	return nil
}

// syncSecrets checks to see if we have updated storage credentials from:
// 1.) user provided credentials in the image-registry-private-configuration-user secret
// and updates the image-registry-private-configuration secret which provides
// those credentials to the registry pod
func (g *Generator) syncSecrets(cr *imageregistryv1.Config, modified *bool) error {
	coreClient, err := clusterconfig.GetCoreClient()
	if err != nil {
		return err
	}

	// Get the existing image-registry-private-configuration secret
	sec, err := coreClient.Secrets(g.params.Deployment.Namespace).Get(imageregistryv1.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get secret %q: %v", fmt.Sprintf("%s/%s", g.params.Deployment.Namespace, imageregistryv1.ImageRegistryPrivateConfiguration), err)
	}

	// Create a driver with the current configuration
	driver, err := storage.NewDriver(cr.Name, cr.Namespace, &cr.Spec.Storage)
	if err != nil {
		return err
	}

	data, err := driver.SyncSecrets(sec)
	if err != nil {
		return err
	}
	if data != nil {
		glog.Infof("Updating secret %q with updated credentials.", fmt.Sprintf("%s/%s", g.params.Deployment.Namespace, imageregistryv1.ImageRegistryPrivateConfiguration))

		// Update the image-registry-private-configuration secret
		_, err := util.CreateOrUpdateSecret(imageregistryv1.ImageRegistryPrivateConfiguration, g.params.Deployment.Namespace, data)
		if err != nil {
			return err
		}
	}

	return nil
}

func (g *Generator) removeObsoleteRoutes(cr *imageregistryv1.Config, modified *bool) error {
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

func (g *Generator) Apply(cr *imageregistryv1.Config, modified *bool) error {
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

	// Make sure that we always sync secrets before we sync storage
	err = g.syncSecrets(cr, modified)
	if err != nil {
		return fmt.Errorf("unable to sync secrets: %s", err)
	}

	err = g.syncStorage(cr, modified)
	if err != nil {
		return fmt.Errorf("unable to sync storage configuration: %s", err)
	}

	return nil
}

func (g *Generator) Remove(cr *imageregistryv1.Config, modified *bool) error {
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
