package operator

import (
	"crypto/rand"
	"fmt"

	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	appsset "github.com/openshift/client-go/apps/clientset/versioned/typed/apps/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration/dependency"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate if no secret
// was specified.
const randomSecretSize = 64

func resourceName(namespace string) string {
	if namespace == "default" {
		return "docker-registry"
	}
	return "image-registry"
}

func (c *Controller) Bootstrap() error {

	crList, err := c.listers.ImageRegistry.List(labels.Everything())
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to list registry custom resources: %s", err)
		}
	}

	switch len(crList) {
	case 0:
		// let's create it.
	case 1:
		return nil
	default:
		return fmt.Errorf("only one registry custom resource expected in %s namespace, got %d", c.params.Deployment.Namespace, len(crList))
	}

	var spec regopapi.ImageRegistrySpec

	appsclient, err := appsset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	dc, err := appsclient.DeploymentConfigs(c.params.Deployment.Namespace).Get(resourceName(c.params.Deployment.Namespace), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		spec = regopapi.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				ManagementState: operatorapi.Managed,
				Version:         "none",
				Logging: operatorapi.LoggingConfig{
					Level: 2,
				},
			},
			Storage:  regopapi.ImageRegistryConfigStorage{},
			TLS:      true,
			Replicas: 1,
		}
	} else if err != nil {
		return fmt.Errorf("unable to check if the deployment already exists: %s", err)
	} else {
		glog.Infof("adopting the existing deployment config...")
		var tlsSecret *corev1.Secret
		spec, tlsSecret, err = migration.NewImageRegistrySpecFromDeploymentConfig(dc, dependency.NewNamespacedResources(c.kubeconfig, dc.ObjectMeta.Namespace))
		if err != nil {
			return fmt.Errorf("unable to adopt the existing deployment config: %s", err)
		}
		if tlsSecret != nil {
			tlsSecret.ObjectMeta = metav1.ObjectMeta{
				Name:      dc.ObjectMeta.Name + "-tls",
				Namespace: dc.ObjectMeta.Namespace,
			}

			coreclient, err := coreset.NewForConfig(c.kubeconfig)
			if err != nil {
				return err
			}

			_, err = coreclient.Secrets(dc.ObjectMeta.Namespace).Create(tlsSecret)
			// TODO: it might already exist
			if err != nil {
				return fmt.Errorf("unable to create the tls secret: %s", err)
			}
		}
	}

	glog.Infof("generating registry custom resource")

	cr := &regopapi.ImageRegistry{
		ObjectMeta: metav1.ObjectMeta{
			Name:       resourceName(c.params.Deployment.Namespace),
			Namespace:  c.params.Deployment.Namespace,
			Finalizers: []string{parameters.ImageRegistryOperatorResourceFinalizer},
		},
		Spec:   spec,
		Status: regopapi.ImageRegistryStatus{},
	}

	if len(cr.Spec.HTTPSecret) == 0 {
		var secretBytes [randomSecretSize]byte
		if _, err := rand.Read(secretBytes[:]); err != nil {
			return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
		}
		cr.Spec.HTTPSecret = fmt.Sprintf("%x", string(secretBytes[:]))
	}

	driver, err := storage.NewDriver(cr.Name, c.params.Deployment.Namespace, &cr.Spec.Storage)
	if err != nil {
		if err != storage.ErrStorageNotConfigured {
			return err
		}
	} else {
		err = driver.CompleteConfiguration(&cr.Status)
		if err != nil {
			return err
		}
	}

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	_, err = client.ImageRegistries().Create(cr)
	return err
}
