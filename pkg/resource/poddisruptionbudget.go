package resource

import (
	"context"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	policyclient "k8s.io/client-go/kubernetes/typed/policy/v1beta1"
	policylisters "k8s.io/client-go/listers/policy/v1beta1"
)

var _ Mutator = &generatorPodDisruptionBudget{}

type generatorPodDisruptionBudget struct {
	lister policylisters.PodDisruptionBudgetNamespaceLister
	client policyclient.PolicyV1beta1Interface
}

func newGeneratorPodDisruptionBudget(lister policylisters.PodDisruptionBudgetNamespaceLister, client policyclient.PolicyV1beta1Interface) *generatorPodDisruptionBudget {
	return &generatorPodDisruptionBudget{
		lister: lister,
		client: client,
	}
}

func (gpdb *generatorPodDisruptionBudget) Type() runtime.Object {
	return &policyv1beta1.PodDisruptionBudget{}
}

func (gpdb *generatorPodDisruptionBudget) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (gpdb *generatorPodDisruptionBudget) GetName() string {
	return "image-registry"
}

func (gpdb *generatorPodDisruptionBudget) expected() (runtime.Object, error) {
	minAvailable := intstr.FromInt(1)

	pdb := &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gpdb.GetName(),
			Namespace: gpdb.GetNamespace(),
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: defaults.DeploymentLabels,
			},
		},
	}

	return pdb, nil
}

func (gpdb *generatorPodDisruptionBudget) Get() (runtime.Object, error) {
	return gpdb.lister.Get(gpdb.GetName())
}

func (gpdb *generatorPodDisruptionBudget) Create() (runtime.Object, error) {
	return commonCreate(gpdb, func(obj runtime.Object) (runtime.Object, error) {
		return gpdb.client.PodDisruptionBudgets(gpdb.GetNamespace()).Create(
			context.TODO(), obj.(*policyv1beta1.PodDisruptionBudget), metav1.CreateOptions{},
		)
	})
}

func (gpdb *generatorPodDisruptionBudget) Update(o runtime.Object) (runtime.Object, bool, error) {
	return commonUpdate(gpdb, o, func(obj runtime.Object) (runtime.Object, error) {
		return gpdb.client.PodDisruptionBudgets(gpdb.GetNamespace()).Update(
			context.TODO(), obj.(*policyv1beta1.PodDisruptionBudget), metav1.UpdateOptions{},
		)
	})
}

func (gpdb *generatorPodDisruptionBudget) Delete(opts metav1.DeleteOptions) error {
	return gpdb.client.PodDisruptionBudgets(gpdb.GetNamespace()).Delete(
		context.TODO(), gpdb.GetName(), opts,
	)
}

func (g *generatorPodDisruptionBudget) Owned() bool {
	return true
}
