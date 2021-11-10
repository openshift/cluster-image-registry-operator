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

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/bindata"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorNodeCADaemonSet{}

type generatorNodeCADaemonSet struct {
	recorder        events.Recorder
	daemonSetLister appsv1listers.DaemonSetNamespaceLister
	serviceLister   corev1listers.ServiceNamespaceLister
	client          appsv1client.AppsV1Interface
	operatorClient  v1helpers.OperatorClient
}

func NewGeneratorNodeCADaemonSet(daemonSetLister appsv1listers.DaemonSetNamespaceLister, serviceLister corev1listers.ServiceNamespaceLister, client appsv1client.AppsV1Interface, operatorClient v1helpers.OperatorClient) Mutator {
	return &generatorNodeCADaemonSet{
		recorder:        events.NewLoggingEventRecorder("image-registry-operator"),
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

func (ds *generatorNodeCADaemonSet) expected() *appsv1.DaemonSet {
	daemonSet := resourceread.ReadDaemonSetV1OrDie(assets.MustAsset("nodecadaemon.yaml"))
	daemonSet.Spec.Template.Spec.Containers[0].Image = os.Getenv("IMAGE")
	return daemonSet
}

func (ds *generatorNodeCADaemonSet) Create() (runtime.Object, error) {
	dep, _, err := ds.Update(nil)
	return dep, err
}

func (ds *generatorNodeCADaemonSet) Update(o runtime.Object) (runtime.Object, bool, error) {
	daemonSet := ds.expected()

	_, opStatus, _, err := ds.operatorClient.GetOperatorState()
	if err != nil {
		return nil, false, err
	}

	dep, updated, err := resourceapply.ApplyDaemonSet(
		context.TODO(),
		ds.client,
		ds.recorder,
		daemonSet,
		resourcemerge.ExpectedDaemonSetGeneration(daemonSet, opStatus.Generations),
	)
	if err != nil {
		return o, updated, err
	}

	if updated {
		updateStatusFn := func(newStatus *operatorv1.OperatorStatus) error {
			resourcemerge.SetDaemonSetGeneration(&newStatus.Generations, daemonSet)
			return nil
		}

		_, _, err = v1helpers.UpdateStatus(
			context.TODO(),
			ds.operatorClient,
			updateStatusFn,
		)
		if err != nil {
			return dep, updated, err
		}
	}

	return dep, updated, nil
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
