package resource

import (
	"bytes"
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
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kappsset "k8s.io/client-go/kubernetes/typed/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
	storageS3 "github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
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

// syncStorage checks:
// 1.)  to make sure that an existing storage medium still exists and we can access it
// 2.)  to see if the storage medium name changed and we need to:
//      a.) check to make sure that we can access the storage or
//      b.) see if we need to try to create the new storage
func (g *Generator) syncStorage(cr *regopapi.ImageRegistry, modified *bool) error {

	// If the Image Registry is currently using S3 storage
	if cr.Spec.Storage.S3 != nil {
		// Create a new driver with the current configuration
		// This saves us from having to duplicate code
		driver := storageS3.NewDriver(cr.Name, cr.Namespace, cr.Spec.Storage.S3)

		// If the bucket name has changed
		if cr.Status.Storage.State.S3 != nil && cr.Spec.Storage.S3.Bucket != cr.Status.Storage.State.S3.Bucket {
			driver.Config.Bucket = cr.Spec.Storage.S3.Bucket
			// Check to see if the bucket exists
			// and if we can access it
			if err := driver.CheckBucketExists(cr); err != nil {
				installConfig, err := clusterconfig.GetInstallConfig()
				if err != nil {
					return err
				}
				// If the bucket doesn't exist, try to create it
				if err := driver.CreateAndTagBucket(installConfig, cr); err != nil {
					return err
				}
				// If we got here, image registry resource
				// was modified by CreateAndTagBucket
				*modified = true
			}
		} else if cr.Spec.Storage.S3.Bucket != "" {
			// If the bucket name didn't change,
			// check to see if the current bucket exists
			if err := driver.CheckBucketExists(cr); err != nil {
				return err
			}
		}
	}
	return nil
}

// syncSecrets checks to see if we have updated storage credentials from:
// 1.) cluster wide cloud credentials
// 2.) user provided credentials in the image-registry-private-configuration-user secret
// and updates the image-registry-private-configuration secret that provides
// those credentials to the registry pod
func (g *Generator) syncSecrets(cr *regopapi.ImageRegistry, modified *bool) error {
	client, err := clusterconfig.GetCoreClient()
	if err != nil {
		return err
	}

	appsclient, err := kappsset.NewForConfig(g.kubeconfig)
	if err != nil {
		return err
	}

	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		return err
	}

	// Get the existing image-registry-private-configuration secret
	sec, err := client.Secrets(g.params.Deployment.Namespace).Get(regopapi.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("unable to get secret %q: %v", fmt.Sprintf("%s/%s", g.params.Deployment.Namespace, regopapi.ImageRegistryPrivateConfiguration), err)
	}

	// If the Image Registry is currently using S3 storage
	if cr.Spec.Storage.S3 != nil {
		// Get the existing SecretKey and AccessKey
		var existingAccessKey, existingSecretKey []byte
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]; ok {
			existingAccessKey = v
		}
		if v, ok := sec.Data["REGISTRY_STORAGE_S3_SECRETKEY"]; ok {
			existingSecretKey = v
		}

		// Check if the existing SecretKey and AccessKey match what we got from the cluster or user configuration
		if !bytes.Equal([]byte(cfg.Storage.S3.AccessKey), existingAccessKey) || !bytes.Equal([]byte(cfg.Storage.S3.SecretKey), existingSecretKey) {

			glog.Infof("Updating secret %q with updated S3 credentials.", fmt.Sprintf("%s/%s", g.params.Deployment.Namespace, regopapi.ImageRegistryPrivateConfiguration))
			data := map[string]string{
				"REGISTRY_STORAGE_S3_ACCESSKEY": cfg.Storage.S3.AccessKey,
				"REGISTRY_STORAGE_S3_SECRETKEY": cfg.Storage.S3.SecretKey,
			}

			// Update the image-registry-private-configuration secret
			upSec, err := util.CreateOrUpdateSecret(regopapi.ImageRegistryPrivateConfiguration, g.params.Deployment.Namespace, data)
			if err != nil {
				return err
			}

			// Generate a checksum of the updated secret
			upSecChecksum, err := strategy.Checksum(upSec)
			if err != nil {
				return nil
			}

			// Get the Image Registry deployment
			deployment, err := appsclient.Deployments(g.params.Deployment.Namespace).Get("image-registry", metaapi.GetOptions{})
			if err != nil {
				return err
			}

			// Make sure that the annotations map isn't nil!
			if deployment.Spec.Template.Annotations == nil {
				deployment.Spec.Template.Annotations = make(map[string]string)
			}

			// Update the deployment template with an annotation of the checksum of the secret
			// to trigger the Image Registry to be redeployed
			deployment.Spec.Template.Annotations[fmt.Sprintf("%s-checksum", upSec.ObjectMeta.Name)] = upSecChecksum
			if _, err := appsclient.Deployments(g.params.Deployment.Namespace).Update(deployment); err != nil {
				return err
			}
		}
	}
	return nil
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
