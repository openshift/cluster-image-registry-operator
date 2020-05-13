package resource

import (
	"context"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	"github.com/openshift/cluster-image-registry-operator/pkg/assets"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorNodeCADaemonSet{}

type generatorNodeCADaemonSet struct {
	daemonSetLister appsv1listers.DaemonSetNamespaceLister
	serviceLister   corev1listers.ServiceNamespaceLister
	client          appsv1client.AppsV1Interface
}

func NewGeneratorNodeCADaemonSet(daemonSetLister appsv1listers.DaemonSetNamespaceLister, serviceLister corev1listers.ServiceNamespaceLister, client appsv1client.AppsV1Interface) Mutator {
	return &generatorNodeCADaemonSet{
		daemonSetLister: daemonSetLister,
		serviceLister:   serviceLister,
		client:          client,
	}
}

func (ds *generatorNodeCADaemonSet) Type() runtime.Object {
	return &appsv1.DaemonSet{}
}

func (ds *generatorNodeCADaemonSet) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (ds *generatorNodeCADaemonSet) GetName() string {
	return "node-ca"
}

func (ds *generatorNodeCADaemonSet) Get() (runtime.Object, error) {
	return ds.daemonSetLister.Get(ds.GetName())
}

func (ds *generatorNodeCADaemonSet) Create() (runtime.Object, error) {
	daemonSet := resourceread.ReadDaemonSetV1OrDie(assets.MustAsset("nodecadaemon.yaml"))
	daemonSet.Spec.Template.Spec.Containers[0].Image = os.Getenv("IMAGE")

	return ds.client.DaemonSets(ds.GetNamespace()).Create(
		context.TODO(), daemonSet, metav1.CreateOptions{},
	)
}

func (ds *generatorNodeCADaemonSet) Update(o runtime.Object) (runtime.Object, bool, error) {
	daemonSet := o.(*appsv1.DaemonSet)
	modified := false

	newImage := os.Getenv("IMAGE")
	oldImage := daemonSet.Spec.Template.Spec.Containers[0].Image
	if newImage != oldImage {
		daemonSet.Spec.Template.Spec.Containers[0].Image = newImage
		modified = true
	}

	if !modified {
		return o, false, nil
	}

	n, err := ds.client.DaemonSets(ds.GetNamespace()).Update(
		context.TODO(), daemonSet, metav1.UpdateOptions{},
	)
	return n, err == nil, err
}

func (ds *generatorNodeCADaemonSet) Delete(opts metav1.DeleteOptions) error {
	return ds.client.DaemonSets(ds.GetNamespace()).Delete(
		context.TODO(), ds.GetName(), opts,
	)
}

func (ds *generatorNodeCADaemonSet) Owned() bool {
	// the nodeca daemon's lifecycle is not tied to the lifecycle of the registry
	return false
}
