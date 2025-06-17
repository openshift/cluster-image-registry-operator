package resource

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/api/annotations"
	operatorv1 "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

var _ Mutator = &generatorImageRegistryCA{}

type generatorImageRegistryCA struct {
	lister                    corelisters.ConfigMapNamespaceLister
	managedLister             corelisters.ConfigMapLister
	imageConfigLister         configlisters.ImageLister
	openshiftConfigLister     corelisters.ConfigMapNamespaceLister
	serviceLister             corelisters.ServiceNamespaceLister
	imageRegistryConfigLister imageregistryv1listers.ConfigLister
	storageListers            *client.StorageListers
	kubeconfig                *restclient.Config
	client                    coreset.CoreV1Interface
	featureGateAccessor       featuregates.FeatureGateAccess
}

func NewGeneratorImageRegistryCA(
	lister corelisters.ConfigMapNamespaceLister,
	managedLister corelisters.ConfigMapLister,
	imageConfigLister configlisters.ImageLister,
	openshiftConfigLister corelisters.ConfigMapNamespaceLister,
	serviceLister corelisters.ServiceNamespaceLister,
	imageRegistryConfigLister imageregistryv1listers.ConfigLister,
	storageListers *client.StorageListers,
	kubeconfig *restclient.Config,
	client coreset.CoreV1Interface,
	featureGateAccessor featuregates.FeatureGateAccess,
) Mutator {
	return &generatorImageRegistryCA{
		lister:                    lister,
		managedLister:             managedLister,
		imageConfigLister:         imageConfigLister,
		openshiftConfigLister:     openshiftConfigLister,
		serviceLister:             serviceLister,
		imageRegistryConfigLister: imageRegistryConfigLister,
		storageListers:            storageListers,
		kubeconfig:                kubeconfig,
		client:                    client,
		featureGateAccessor:       featureGateAccessor,
	}
}

func (girca *generatorImageRegistryCA) Type() runtime.Object {
	return &corev1.ConfigMap{}
}

func (girca *generatorImageRegistryCA) GetNamespace() string {
	return defaults.OpenShiftConfigManagedNamespace
}

func (girca *generatorImageRegistryCA) GetName() string {
	return defaults.ImageRegistryCAName
}

func (girca *generatorImageRegistryCA) storageDriver() (storage.Driver, bool, error) {
	imageRegistryConfig, err := girca.imageRegistryConfigLister.Get("cluster")
	if errors.IsNotFound(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	if imageRegistryConfig.Spec.ManagementState == operatorv1.Removed {
		// The certificates controller does not need to know about
		// storage when the management state is Removed.
		return nil, false, nil
	}

	driver, err := storage.NewDriver(&imageRegistryConfig.Spec.Storage, girca.kubeconfig, girca.storageListers, girca.featureGateAccessor)
	if err == storage.ErrStorageNotConfigured || storage.IsMultiStoragesError(err) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, err
	}

	canRedirect := !imageRegistryConfig.Spec.DisableRedirect

	return driver, canRedirect, nil
}

func (girca *generatorImageRegistryCA) expected() (runtime.Object, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      girca.GetName(),
			Namespace: girca.GetNamespace(),
			Annotations: map[string]string{
				annotations.OpenShiftComponent: "Image Registry",
			},
		},
		Data:       map[string]string{},
		BinaryData: map[string][]byte{},
	}

	var ownHostnameKeys []string

	serviceCA, err := girca.lister.Get(defaults.ServiceCAName)
	if errors.IsNotFound(err) {
		klog.V(4).Infof("missing the service CA configmap: %s", err)
	} else if err != nil {
		return cm, fmt.Errorf("%s: %s", girca.GetName(), err)
	} else {
		if cert, ok := serviceCA.Data["service-ca.crt"]; ok {
			internalHostnames, err := getServiceHostnames(girca.serviceLister, defaults.ServiceName)
			if err != nil {
				return cm, fmt.Errorf("%s: %s", girca.GetName(), err)
			}
			if len(internalHostnames) == 0 {
				klog.Infof("unable to get the service name to add service-ca.crt")
			} else {
				for _, internalHostname := range internalHostnames {
					key := strings.Replace(internalHostname, ":", "..", -1)
					ownHostnameKeys = append(ownHostnameKeys, key)
					cm.Data[key] = cert
				}
			}
		} else {
			klog.Infof("the service CA is not injected yet")
		}
	}

	driver, canRedirect, err := girca.storageDriver()
	if err != nil {
		return cm, fmt.Errorf("%s: %s", girca.GetName(), err)
	}
	if driver != nil {
		storageCABundle, _, err := driver.CABundle()
		if err != nil {
			return cm, fmt.Errorf("%s: %s", girca.GetName(), err)
		}
		if storageCABundle != "" {
			klog.V(4).Infof("using storage ca bundle (%d bytes)", len(storageCABundle))
			if canRedirect {
				klog.V(4).Infof("injecting storage ca bundle into registry certificates...")
				for _, key := range ownHostnameKeys {
					cm.Data[key] += "\n" + storageCABundle
				}
			}
		}
	}

	return cm, nil
}

func (girca *generatorImageRegistryCA) Get() (runtime.Object, error) {
	return girca.managedLister.ConfigMaps(defaults.OpenShiftConfigManagedNamespace).Get(girca.GetName())
}

func (girca *generatorImageRegistryCA) Create() (runtime.Object, error) {
	return commonCreate(girca, func(obj runtime.Object) (runtime.Object, error) {
		return girca.client.ConfigMaps(girca.GetNamespace()).Create(
			context.TODO(), obj.(*corev1.ConfigMap), metav1.CreateOptions{},
		)
	})
}

func (girca *generatorImageRegistryCA) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(girca, o, func(obj runtime.Object) (runtime.Object, error) {
		return girca.client.ConfigMaps(girca.GetNamespace()).Update(
			context.TODO(), obj.(*corev1.ConfigMap), metav1.UpdateOptions{},
		)
	})
}

func (girca *generatorImageRegistryCA) Delete(opts metav1.DeleteOptions) error {
	return girca.client.ConfigMaps(girca.GetNamespace()).Delete(
		context.TODO(), girca.GetName(), opts,
	)
}

func (g *generatorImageRegistryCA) Owned() bool {
	return true
}
