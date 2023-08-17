package resource

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appsv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	appsv1listers "k8s.io/client-go/listers/apps/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorNodeCADaemonSet{}

type generatorNodeCADaemonSet struct {
	eventRecorder   events.Recorder
	daemonSetLister appsv1listers.DaemonSetNamespaceLister
	serviceLister   corev1listers.ServiceNamespaceLister
	client          appsv1client.AppsV1Interface
	operatorClient  v1helpers.OperatorClient
}

func NewGeneratorNodeCADaemonSet(eventRecorder events.Recorder, daemonSetLister appsv1listers.DaemonSetNamespaceLister, serviceLister corev1listers.ServiceNamespaceLister, client appsv1client.AppsV1Interface, operatorClient v1helpers.OperatorClient) Mutator {
	return &generatorNodeCADaemonSet{
		eventRecorder:   eventRecorder,
		daemonSetLister: daemonSetLister,
		serviceLister:   serviceLister,
		client:          client,
		operatorClient:  operatorClient,
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
	return nil, nil
}

func (ds *generatorNodeCADaemonSet) Update(o runtime.Object) (runtime.Object, bool, error) {
	return nil, false, nil
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
