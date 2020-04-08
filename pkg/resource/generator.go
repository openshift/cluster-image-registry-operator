package resource

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/object"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

func ApplyMutator(gen Mutator) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		o, err := gen.Get()
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get object %s: %s", Name(gen), err)
			}

			n, err := gen.Create()
			if err != nil {
				return fmt.Errorf("failed to create object %s: %s", Name(gen), err)
			}

			str, err := object.DumpString(n)
			if err != nil {
				klog.Errorf("unable to dump object: %s", err)
			}

			klog.Infof("object %s created: %s", Name(gen), str)
			return nil
		}

		n, updated, err := gen.Update(o.DeepCopyObject())
		if err != nil {
			if errors.IsConflict(err) {
				return err
			}
			return fmt.Errorf("failed to update object %s: %s", Name(gen), err)
		}
		if updated {
			difference, err := object.DiffString(o, n)
			if err != nil {
				klog.Errorf("unable to calculate difference: %s", err)
			}
			klog.Infof("object %s updated: %s", Name(gen), difference)
		}
		return nil
	})
}

func NewGenerator(kubeconfig *rest.Config, clients *client.Clients, listers *client.Listers) *Generator {
	return &Generator{
		kubeconfig: kubeconfig,
		listers:    listers,
		clients:    clients,
	}
}

type Generator struct {
	kubeconfig *rest.Config
	listers    *client.Listers
	clients    *client.Clients
}

func (g *Generator) listRoutes(cr *imageregistryv1.Config) []Mutator {
	var mutators []Mutator
	if cr.Spec.DefaultRoute {
		mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, g.clients.Route, cr, imageregistryv1.ImageRegistryConfigRoute{
			Name: defaults.RouteName,
		}))
	}
	for _, route := range cr.Spec.Routes {
		mutators = append(mutators, newGeneratorRoute(g.listers.Routes, g.listers.Secrets, g.clients.Route, cr, route))
	}
	return mutators
}

func (g *Generator) List(cr *imageregistryv1.Config) ([]Mutator, error) {
	driver, err := storage.NewDriver(&cr.Spec.Storage, g.kubeconfig, g.listers)
	if err != nil && err != storage.ErrStorageNotConfigured {
		return nil, err
	} else if err == storage.ErrStorageNotConfigured {
		klog.V(6).Info("storage not configured, some mutators might not work.")
	}

	var mutators []Mutator
	mutators = append(mutators, newGeneratorClusterRole(g.listers.ClusterRoles, g.clients.RBAC))
	mutators = append(mutators, newGeneratorClusterRoleBinding(g.listers.ClusterRoleBindings, g.clients.RBAC))
	mutators = append(mutators, newGeneratorServiceAccount(g.listers.ServiceAccounts, g.clients.Core))
	mutators = append(mutators, newGeneratorServiceCA(g.listers.ConfigMaps, g.clients.Core))
	mutators = append(mutators, newGeneratorPullSecret(g.clients.Core))
	mutators = append(mutators, newGeneratorSecret(g.listers.Secrets, g.clients.Core, driver))
	mutators = append(mutators, newGeneratorImageConfig(g.listers.ImageConfigs, g.listers.Routes, g.listers.Services, g.clients.Config))
	mutators = append(mutators, newGeneratorService(g.listers.Services, g.clients.Core))
	mutators = append(mutators, newGeneratorDeployment(g.listers.Deployments, g.listers.ConfigMaps, g.listers.Secrets, g.listers.ProxyConfigs, g.clients.Core, g.clients.Apps, driver, cr))
	mutators = append(mutators, g.listRoutes(cr)...)

	return mutators, nil
}

// syncStorage checks:
// 1.)  to make sure that an existing storage medium still exists and we can access it
// 2.)  to see if the storage medium name changed and we need to:
//      a.) check to make sure that we can access the storage or
//      b.) see if we need to try to create the new storage
func (g *Generator) syncStorage(cr *imageregistryv1.Config) error {
	var runCreate bool
	// Create a driver with the current configuration
	driver, err := storage.NewDriver(&cr.Spec.Storage, g.kubeconfig, g.listers)
	if err == storage.ErrStorageNotConfigured {
		cr.Spec.Storage, _, err = storage.GetPlatformStorage(g.listers)
		if err != nil {
			return fmt.Errorf("unable to get storage configuration from cluster install config: %s", err)
		}
		driver, err = storage.NewDriver(&cr.Spec.Storage, g.kubeconfig, g.listers)
	}
	if err != nil {
		return err
	}

	if driver.StorageChanged(cr) {
		runCreate = true
	} else {
		exists, err := driver.StorageExists(cr)
		if err != nil {
			return err
		}
		if !exists {
			runCreate = true
		}
	}

	if runCreate {
		reconf := g.storageReconfigured(cr, g.kubeconfig, g.listers)
		if err := driver.CreateStorage(cr); err != nil {
			return err
		}
		if reconf {
			metrics.StorageReconfigured()
		}
	}

	return nil
}

// storageReconfigured returns true if we are, based on the provided config,
// starting to use a different underlying storage location.
func (g *Generator) storageReconfigured(
	regCfg *imageregistryv1.Config,
	restCfg *rest.Config,
	listers *client.Listers,
) bool {
	prev, err := storage.NewDriver(&regCfg.Status.Storage, restCfg, listers)
	if err != nil {
		return false
	}
	cur, err := storage.NewDriver(&regCfg.Spec.Storage, restCfg, listers)
	if err != nil {
		return false
	}

	if reflect.TypeOf(prev) != reflect.TypeOf(cur) {
		return true
	}

	return prev.ID() != cur.ID()
}

func (g *Generator) removeObsoleteRoutes(cr *imageregistryv1.Config) error {
	routes, err := g.listers.Routes.List(labels.Everything())
	if err != nil {
		return fmt.Errorf("failed to list routes: %s", err)
	}

	routesGenerators := g.listRoutes(cr)
	knownNames := map[string]struct{}{}
	for _, gen := range routesGenerators {
		knownNames[gen.GetName()] = struct{}{}
	}

	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground
	opts := metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	}
	for _, route := range routes {
		if !RouteIsCreatedByOperator(route) {
			continue
		}
		if _, found := knownNames[route.Name]; found {
			continue
		}
		err = g.clients.Route.Routes(defaults.ImageRegistryOperatorNamespace).Delete(
			context.TODO(), route.Name, opts,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (g *Generator) Apply(cr *imageregistryv1.Config) error {
	err := g.syncStorage(cr)
	if err == storage.ErrStorageNotConfigured {
		return err
	} else if err != nil {
		return fmt.Errorf("unable to sync storage configuration: %s", err)
	}

	generators, err := g.List(cr)
	if err != nil {
		return fmt.Errorf("unable to get generators: %s", err)
	}

	for _, gen := range generators {
		err = ApplyMutator(gen)
		if err != nil {
			return fmt.Errorf("unable to apply objects: %s", err)
		}
	}

	err = g.removeObsoleteRoutes(cr)
	if err != nil {
		return fmt.Errorf("unable to remove obsolete routes: %s", err)
	}

	return nil
}

func (g *Generator) Remove(cr *imageregistryv1.Config) error {
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

	driver, err := storage.NewDriver(&cr.Status.Storage, g.kubeconfig, g.listers)
	if err == storage.ErrStorageNotConfigured {
		return nil
	} else if err != nil {
		return err
	}

	var derr error
	var retriable bool
	err = wait.PollImmediate(1*time.Second, 5*time.Minute, func() (stop bool, err error) {
		if retriable, derr = driver.RemoveStorage(cr); derr != nil {
			if retriable {
				return false, nil
			} else {
				return true, derr
			}
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("unable to remove storage: %s, %s", err, derr)
	}

	cr.Status.Storage = imageregistryv1.ImageRegistryConfigStorage{}

	return nil
}
