package resource

import (
	"os"

	appsapi "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

var _ Mutator = &generatorDeployment{}

type generatorDeployment struct {
	lister          appslisters.DeploymentNamespaceLister
	configMapLister corelisters.ConfigMapNamespaceLister
	secretLister    corelisters.SecretNamespaceLister
	proxyLister     configlisters.ProxyLister
	coreClient      coreset.CoreV1Interface
	client          appsset.AppsV1Interface
	driver          storage.Driver
	params          *parameters.Globals
	cr              *imageregistryv1.Config
}

func newGeneratorDeployment(lister appslisters.DeploymentNamespaceLister, configMapLister corelisters.ConfigMapNamespaceLister, secretLister corelisters.SecretNamespaceLister, proxyLister configlisters.ProxyLister, coreClient coreset.CoreV1Interface, client appsset.AppsV1Interface, driver storage.Driver, params *parameters.Globals, cr *imageregistryv1.Config) *generatorDeployment {
	return &generatorDeployment{
		lister:          lister,
		configMapLister: configMapLister,
		secretLister:    secretLister,
		proxyLister:     proxyLister,
		coreClient:      coreClient,
		client:          client,
		driver:          driver,
		params:          params,
		cr:              cr,
	}
}

func (gd *generatorDeployment) Type() runtime.Object {
	return &appsapi.Deployment{}
}

func (gd *generatorDeployment) GetGroup() string {
	return appsapi.GroupName
}

func (gd *generatorDeployment) GetResource() string {
	return "deployments"
}

func (gd *generatorDeployment) GetNamespace() string {
	return gd.params.Deployment.Namespace
}

func (gd *generatorDeployment) GetName() string {
	return defaults.ImageRegistryName
}

func (gd *generatorDeployment) expected() (runtime.Object, error) {
	podTemplateSpec, deps, err := makePodTemplateSpec(gd.coreClient, gd.proxyLister, gd.driver, gd.params, gd.cr)
	if err != nil {
		return nil, err
	}

	depsChecksum, err := deps.Checksum(gd.configMapLister, gd.secretLister)
	if err != nil {
		return nil, err
	}

	if podTemplateSpec.Annotations == nil {
		podTemplateSpec.Annotations = map[string]string{}
	}
	podTemplateSpec.Annotations[parameters.ChecksumOperatorDepsAnnotation] = depsChecksum

	deploy := &appsapi.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gd.GetName(),
			Namespace: gd.GetNamespace(),
			Labels:    gd.params.Deployment.Labels,
			Annotations: map[string]string{
				defaults.VersionAnnotation: os.Getenv("RELEASE_VERSION"),
			},
		},
		Spec: appsapi.DeploymentSpec{
			Replicas: &gd.cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: gd.params.Deployment.Labels,
			},
			Template: podTemplateSpec,
		},
	}

	return deploy, nil
}

func (gd *generatorDeployment) Get() (runtime.Object, error) {
	return gd.lister.Get(gd.GetName())
}

func (gd *generatorDeployment) Create() (runtime.Object, error) {
	return commonCreate(gd, func(obj runtime.Object) (runtime.Object, error) {
		return gd.client.Deployments(gd.GetNamespace()).Create(obj.(*appsapi.Deployment))
	})
}

func (gd *generatorDeployment) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gd, o, func(obj runtime.Object) (runtime.Object, error) {
		return gd.client.Deployments(gd.GetNamespace()).Update(obj.(*appsapi.Deployment))
	})
}

func (gd *generatorDeployment) Delete(opts *metav1.DeleteOptions) error {
	return gd.client.Deployments(gd.GetNamespace()).Delete(gd.GetName(), opts)
}

func (g *generatorDeployment) Owned() bool {
	return true
}
