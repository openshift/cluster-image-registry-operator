package resource

import (
	appsapi "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var _ Mutator = &generatorDeployment{}

type generatorDeployment struct {
	lister          appslisters.DeploymentNamespaceLister
	configMapLister corelisters.ConfigMapNamespaceLister
	secretLister    corelisters.SecretNamespaceLister
	coreClient      coreset.CoreV1Interface
	client          appsset.AppsV1Interface
	params          *parameters.Globals
	cr              *regopapi.ImageRegistry
}

func newGeneratorDeployment(lister appslisters.DeploymentNamespaceLister, configMapLister corelisters.ConfigMapNamespaceLister, secretLister corelisters.SecretNamespaceLister, coreClient coreset.CoreV1Interface, client appsset.AppsV1Interface, params *parameters.Globals, cr *regopapi.ImageRegistry) *generatorDeployment {
	return &generatorDeployment{
		lister:          lister,
		configMapLister: configMapLister,
		secretLister:    secretLister,
		coreClient:      coreClient,
		client:          client,
		params:          params,
		cr:              cr,
	}
}

func (gd *generatorDeployment) Type() runtime.Object {
	return &appsapi.Deployment{}
}

func (gd *generatorDeployment) GetNamespace() string {
	return gd.params.Deployment.Namespace
}

func (gd *generatorDeployment) GetName() string {
	return gd.cr.ObjectMeta.Name
}

func (gd *generatorDeployment) expected() (runtime.Object, error) {
	podTemplateSpec, deps, err := makePodTemplateSpec(gd.coreClient, gd.params, gd.cr)
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
		},
		Spec: appsapi.DeploymentSpec{
			Replicas: &gd.cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: gd.params.Deployment.Labels,
			},
			Template: podTemplateSpec,
		},
	}

	addOwnerRefToObject(deploy, asOwner(gd.cr))

	return deploy, nil
}

func (gd *generatorDeployment) Get() (runtime.Object, error) {
	return gd.lister.Get(gd.GetName())
}

func (gd *generatorDeployment) Create() error {
	return commonCreate(gd, func(obj runtime.Object) (runtime.Object, error) {
		return gd.client.Deployments(gd.GetNamespace()).Create(obj.(*appsapi.Deployment))
	})
}

func (gd *generatorDeployment) Update(o runtime.Object) (bool, error) {
	return commonUpdate(gd, o, func(obj runtime.Object) (runtime.Object, error) {
		return gd.client.Deployments(gd.GetNamespace()).Update(obj.(*appsapi.Deployment))
	})
}

func (gd *generatorDeployment) Delete(opts *metav1.DeleteOptions) error {
	return gd.client.Deployments(gd.GetNamespace()).Delete(gd.GetName(), opts)
}
