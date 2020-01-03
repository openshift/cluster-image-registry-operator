package operator

import (
	"crypto/rand"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// randomSecretSize is the number of random bytes to generate
// for the http secret
const randomSecretSize = 64

// Bootstrap registers this operator with OpenShift by creating an appropriate
// ClusterOperator custom resource. This function also creates the initial
// configuration for the Image Registry.
func (c *Controller) Bootstrap() error {
	cr, err := c.listers.RegistryConfigs.Get(defaults.ImageRegistryResourceName)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to get the registry custom resources: %s", err)
	}

	// If the registry resource already exists,
	// no bootstrapping is required
	if cr != nil {
		return nil
	}

	// If no registry resource exists,
	// let's create one with sane defaults
	klog.Infof("generating registry custom resource")

	var secretBytes [randomSecretSize]byte
	if _, err := rand.Read(secretBytes[:]); err != nil {
		return fmt.Errorf("could not generate random bytes for HTTP secret: %s", err)
	}

	platformStorage, err := storage.GetPlatformStorage(c.listers)
	if err != nil {
		return err
	}

	noStorage := imageregistryv1.ImageRegistryConfigStorage{}

	// We bootstrap as "Removed" if the platform is known and does not
	// provide persistent storage out of the box. If the platform is
	// unknown we will bootstrap as Managed but using EmptyDir storage
	// engine(ephemeral).
	mgmtState := operatorapi.Managed
	if platformStorage == noStorage {
		mgmtState = operatorapi.Removed
	}

	cr = &imageregistryv1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name:       defaults.ImageRegistryResourceName,
			Namespace:  c.params.Deployment.Namespace,
			Finalizers: []string{parameters.ImageRegistryOperatorResourceFinalizer},
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: mgmtState,
			LogLevel:        2,
			Storage:         imageregistryv1.ImageRegistryConfigStorage{},
			Replicas:        1,
			HTTPSecret:      fmt.Sprintf("%x", string(secretBytes[:])),
		},
		Status: imageregistryv1.ImageRegistryStatus{},
	}

	if genErr := c.generator.ApplyClusterOperator(cr); genErr != nil {
		klog.Errorf("unable to apply cluster operator (bootstrap): %s", genErr)
	}

	client, err := regopset.NewForConfig(c.kubeconfig)
	if err != nil {
		return err
	}

	_, err = client.Configs().Create(cr)
	if err != nil {
		return err
	}

	return nil
}
