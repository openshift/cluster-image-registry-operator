package resource

import (
	"context"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	networkingv1client "k8s.io/client-go/kubernetes/typed/networking/v1"
	networkingv1listers "k8s.io/client-go/listers/networking/v1"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"

	assets "github.com/openshift/cluster-image-registry-operator/bindata"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorImagePrunerNetworkPolicy{}

type generatorImagePrunerNetworkPolicy struct {
	eventRecorder       events.Recorder
	networkPolicyLister networkingv1listers.NetworkPolicyNamespaceLister
	client              networkingv1client.NetworkingV1Interface
	cache               resourceapply.ResourceCache
}

func newGeneratorImagePrunerNetworkPolicy(eventRecorder events.Recorder, networkPolicyLister networkingv1listers.NetworkPolicyNamespaceLister, client networkingv1client.NetworkingV1Interface, cache resourceapply.ResourceCache) Mutator {
	return &generatorImagePrunerNetworkPolicy{
		eventRecorder:       eventRecorder,
		networkPolicyLister: networkPolicyLister,
		client:              client,
		cache:               cache,
	}
}

func (np *generatorImagePrunerNetworkPolicy) Type() runtime.Object {
	return &networkingv1.NetworkPolicy{}
}

func (np *generatorImagePrunerNetworkPolicy) GetNamespace() string {
	return defaults.ImageRegistryOperatorNamespace
}

func (np *generatorImagePrunerNetworkPolicy) GetName() string {
	return "image-pruner-networkpolicy"
}

func (np *generatorImagePrunerNetworkPolicy) Get() (runtime.Object, error) {
	return np.networkPolicyLister.Get(np.GetName())
}

func (np *generatorImagePrunerNetworkPolicy) expected() *networkingv1.NetworkPolicy {
	networkPolicy := resourceread.ReadNetworkPolicyV1OrDie(assets.MustAsset("image-pruner-networkpolicy.yaml"))
	return networkPolicy
}

func (np *generatorImagePrunerNetworkPolicy) Create() (runtime.Object, error) {
	obj, _, err := np.Update(nil)
	return obj, err
}

func (np *generatorImagePrunerNetworkPolicy) Update(o runtime.Object) (runtime.Object, bool, error) {
	desiredNetworkPolicy := np.expected()

	actualNetworkPolicy, updated, err := resourceapply.ApplyNetworkPolicy(
		context.TODO(),
		np.client,
		np.eventRecorder,
		desiredNetworkPolicy,
		np.cache,
	)
	if err != nil {
		return o, updated, err
	}

	return actualNetworkPolicy, updated, nil
}

func (np *generatorImagePrunerNetworkPolicy) Delete(opts metav1.DeleteOptions) error {
	return np.client.NetworkPolicies(np.GetNamespace()).Delete(
		context.TODO(), np.GetName(), opts,
	)
}

func (np *generatorImagePrunerNetworkPolicy) Owned() bool {
	// the network policy lifecycle is tied to the lifecycle of the registry
	return true
}
