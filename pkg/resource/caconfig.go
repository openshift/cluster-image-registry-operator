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
	"k8s.io/klog/v2"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorCAConfig{}

type generatorCAConfig struct {
	lister                corelisters.ConfigMapNamespaceLister
	imageConfigLister     configlisters.ImageLister
	openshiftConfigLister corelisters.ConfigMapNamespaceLister
	serviceLister         corelisters.ServiceNamespaceLister
	client                coreset.CoreV1Interface
}

func NewGeneratorCAConfig(lister corelisters.ConfigMapNamespaceLister, imageConfigLister configlisters.ImageLister, openshiftConfigLister corelisters.ConfigMapNamespaceLister, serviceLister corelisters.ServiceNamespaceLister, client coreset.CoreV1Interface) Mutator {
	return &generatorCAConfig{
		lister:                lister,
		imageConfigLister:     imageConfigLister,
		openshiftConfigLister: openshiftConfigLister,
		serviceLister:         serviceLister,
		client:                client,
	}
}

func (gcac *generatorCAConfig) Type() runtime.Object {
	return &corev1.ConfigMap{}
}

func (gcac *generatorCAConfig) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (gcac *generatorCAConfig) GetName() string {
	return defaults.ImageRegistryCertificatesName
}

func (gcac *generatorCAConfig) expected() (runtime.Object, error) {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcac.GetName(),
			Namespace: gcac.GetNamespace(),
		},
		Data:       map[string]string{},
		BinaryData: map[string][]byte{},
	}

	serviceCA, err := gcac.lister.Get(defaults.ServiceCAName)
	if errors.IsNotFound(err) {
		klog.V(4).Infof("missing the service CA configmap: %s", err)
	} else if err != nil {
		return cm, err
	} else {
		if cert, ok := serviceCA.Data["service-ca.crt"]; ok {
			internalHostnames, err := getServiceHostnames(gcac.serviceLister, defaults.ServiceName)
			if err != nil {
				return cm, err
			}
			if len(internalHostnames) == 0 {
				klog.Infof("unable to get the service name to add service-ca.crt")
			} else {
				for _, internalHostname := range internalHostnames {
					cm.Data[strings.Replace(internalHostname, ":", "..", -1)] = cert
				}
			}
		} else {
			klog.Infof("the service CA is not injected yet")
		}
	}

	imageConfig, err := gcac.imageConfigLister.Get(defaults.ImageConfigName)
	if errors.IsNotFound(err) {
		klog.V(4).Infof("missing the image config: %s", err)
	} else if err != nil {
		return cm, err
	} else if caConfigName := imageConfig.Spec.AdditionalTrustedCA.Name; caConfigName != "" {
		upstreamConfig, err := gcac.openshiftConfigLister.Get(caConfigName)
		if err != nil {
			return nil, err
		}

		for k, v := range upstreamConfig.Data {
			cm.Data[k] = v
		}
		for k, v := range upstreamConfig.BinaryData {
			cm.BinaryData[k] = v
		}
	}

	cp_ca, err := gcac.openshiftConfigLister.Get("cloud-provider-config")
	if errors.IsNotFound(err) {
		klog.V(4).Infof("missing the cloud-provider-config configmap: %s", err)
	} else if err != nil {
		return cm, err
	} else {
		if cert, ok := cp_ca.Data["ca-bundle.pem"]; ok {
			cm.Data["cloud-provider-ca-bundle.pem"] = cert
		}
	}

	return cm, nil
}

func (gcac *generatorCAConfig) Get() (runtime.Object, error) {
	return gcac.lister.Get(gcac.GetName())
}

func (gcac *generatorCAConfig) Create() (runtime.Object, error) {
	return commonCreate(gcac, func(obj runtime.Object) (runtime.Object, error) {
		return gcac.client.ConfigMaps(gcac.GetNamespace()).Create(
			context.TODO(), obj.(*corev1.ConfigMap), metav1.CreateOptions{},
		)
	})
}

func (gcac *generatorCAConfig) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gcac, o, func(obj runtime.Object) (runtime.Object, error) {
		return gcac.client.ConfigMaps(gcac.GetNamespace()).Update(
			context.TODO(), obj.(*corev1.ConfigMap), metav1.UpdateOptions{},
		)
	})
}

func (gcac *generatorCAConfig) Delete(opts metav1.DeleteOptions) error {
	return gcac.client.ConfigMaps(gcac.GetNamespace()).Delete(
		context.TODO(), gcac.GetName(), opts,
	)
}

func (g *generatorCAConfig) Owned() bool {
	return true
}

func getServiceHostnames(serviceLister corelisters.ServiceNamespaceLister, serviceName string) ([]string, error) {
	svc, err := serviceLister.Get(serviceName)
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	port := ""
	if svc.Spec.Ports[0].Port != 443 {
		port = fmt.Sprintf(":%d", svc.Spec.Ports[0].Port)
	}
	return []string{
		fmt.Sprintf("%s.%s.svc%s", svc.Name, svc.Namespace, port),
		fmt.Sprintf("%s.%s.svc.cluster.local%s", svc.Name, svc.Namespace, port),
	}, nil
}
