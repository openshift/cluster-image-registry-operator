package resource

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	imageregistryv1clientset "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"
	imageregistryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

var (
	defaultPrunerSuspend                    = false
	defaultPrunerKeepTagRevisions           = 3
	defaultPrunerSuccessfulJobsHistoryLimit = int32(3)
	defaultPrunerFailedJobsHistoryLimit     = int32(3)
)

var _ Mutator = &generatorPrunerResource{}

type generatorPrunerResource struct {
	lister imageregistryv1listers.ImagePrunerLister
	client imageregistryv1clientset.ImageregistryV1Interface
	cr     *imageregistryv1.ImagePruner
}

func newGeneratorPrunerResource(lister imageregistryv1listers.ImagePrunerLister, client imageregistryv1clientset.ImageregistryV1Interface, params *parameters.Globals) *generatorPrunerResource {
	return &generatorPrunerResource{
		lister: lister,
		client: client,
	}
}

func (gpr *generatorPrunerResource) Type() runtime.Object {
	return &imageregistryv1.ImagePruner{}
}

func (gpr *generatorPrunerResource) GetGroup() string {
	return imageregistryv1.SchemeGroupVersion.Group
}

func (gpr *generatorPrunerResource) GetResource() string {
	return ""
}

func (gpr *generatorPrunerResource) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (gpr *generatorPrunerResource) GetName() string {
	return defaults.ImageRegistryImagePrunerResourceName
}

func (gpr *generatorPrunerResource) expected() (runtime.Object, error) {
	cj := &imageregistryv1.ImagePruner{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpr.GetName(),
			Namespace: gpr.GetNamespace(),
		},
		Spec: imageregistryv1.ImagePrunerSpec{
			Suspend:                    &defaultPrunerSuspend,
			KeepTagRevisions:           &defaultPrunerKeepTagRevisions,
			SuccessfulJobsHistoryLimit: &defaultPrunerSuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     &defaultPrunerFailedJobsHistoryLimit,
		},
		Status: imageregistryv1.ImagePrunerStatus{},
	}

	return cj, nil
}

func (gpr *generatorPrunerResource) Get() (runtime.Object, error) {
	return gpr.lister.Get(gpr.GetName())
}

func (gpr *generatorPrunerResource) Create() (runtime.Object, error) {
	return commonCreate(gpr, func(obj runtime.Object) (runtime.Object, error) {
		return gpr.client.ImagePruners().Create(obj.(*imageregistryv1.ImagePruner))
	})
}

func (gpr *generatorPrunerResource) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gpr, o, func(obj runtime.Object) (runtime.Object, error) {
		return gpr.client.ImagePruners().Update(obj.(*imageregistryv1.ImagePruner))
	})
}

func (gpr *generatorPrunerResource) Delete(opts *metav1.DeleteOptions) error {
	return gpr.client.ImagePruners().Delete(gpr.GetName(), opts)
}

func (gpr *generatorPrunerResource) Owned() bool {
	return true
}
