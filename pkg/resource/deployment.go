package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	appsapi "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/utils/ptr"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	securityv1 "github.com/openshift/api/security/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/resource/strategy"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

var _ Mutator = &generatorDeployment{}

type generatorDeployment struct {
	eventRecorder   events.Recorder
	lister          appslisters.DeploymentNamespaceLister
	configMapLister corelisters.ConfigMapNamespaceLister
	secretLister    corelisters.SecretNamespaceLister
	proxyLister     configlisters.ProxyLister
	coreClient      coreset.CoreV1Interface
	client          appsset.AppsV1Interface
	driver          storage.Driver
	cr              *imageregistryv1.Config
}

func newGeneratorDeployment(eventRecorder events.Recorder, lister appslisters.DeploymentNamespaceLister, configMapLister corelisters.ConfigMapNamespaceLister, secretLister corelisters.SecretNamespaceLister, proxyLister configlisters.ProxyLister, coreClient coreset.CoreV1Interface, client appsset.AppsV1Interface, driver storage.Driver, cr *imageregistryv1.Config) *generatorDeployment {
	return &generatorDeployment{
		eventRecorder:   eventRecorder,
		lister:          lister,
		configMapLister: configMapLister,
		secretLister:    secretLister,
		proxyLister:     proxyLister,
		coreClient:      coreClient,
		client:          client,
		driver:          driver,
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
	return defaults.ImageRegistryOperatorNamespace
}

func (gd *generatorDeployment) GetName() string {
	return defaults.ImageRegistryName
}

func (gd *generatorDeployment) expected() (runtime.Object, error) {
	if gd.driver == nil {
		return nil, fmt.Errorf("no storage driver present")
	}

	podTemplateSpec, deps, err := makePodTemplateSpec(gd.coreClient, gd.proxyLister, gd.driver, gd.cr)
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
	podTemplateSpec.Annotations[defaults.ChecksumOperatorDepsAnnotation] = depsChecksum
	podTemplateSpec.Annotations[securityv1.RequiredSCCAnnotation] = "restricted-v2"

	// Strategy defaults to RollingUpdate
	deployStrategy := appsapi.DeploymentStrategyType(gd.cr.Spec.RolloutStrategy)
	if deployStrategy == "" {
		deployStrategy = appsapi.RollingUpdateDeploymentStrategyType
	}

	var rollingUpdate *appsapi.RollingUpdateDeployment
	if deployStrategy == appsapi.RollingUpdateDeploymentStrategyType {
		if gd.cr.Spec.Replicas == 2 {
			maxUnavailable := intstr.Parse("1")
			maxSurge := intstr.Parse("1")
			rollingUpdate = &appsapi.RollingUpdateDeployment{
				MaxUnavailable: &maxUnavailable,
				MaxSurge:       &maxSurge,
			}
		} else {
			// The deployment controller scales up in an interesting way if the pod
			// template has changed:
			//
			// 1. it scales up the replica set for the old pod template,
			// 2. starts migration to the new pod template according to rolling
			//    update parameters.
			//
			// To scale up from 2 replicas (when the registry pods have hard
			// anti-affinity rules) to 6 replicas on a minimal cluster with 2
			// worker nodes the deployment should tolerate 5 unavailable replicas:
			//
			//  * 4 replicas out of 6 cannot fit onto 2 workers,
			//  * 1 replica should be deleted before a new one can be created.
			maxUnavailable := intstr.FromInt(int(gd.cr.Spec.Replicas) - 1)
			maxSurge := intstr.FromString("25%")
			rollingUpdate = &appsapi.RollingUpdateDeployment{
				MaxUnavailable: &maxUnavailable,
				MaxSurge:       &maxSurge,
			}
		}
	}

	deploy := &appsapi.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gd.GetName(),
			Namespace: gd.GetNamespace(),
			Labels:    defaults.DeploymentLabels,
			Annotations: map[string]string{
				defaults.VersionAnnotation: os.Getenv("RELEASE_VERSION"),
			},
		},
		Spec: appsapi.DeploymentSpec{
			ProgressDeadlineSeconds: ptr.To[int32](60),
			Replicas:                &gd.cr.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: defaults.DeploymentLabels,
			},
			Template: podTemplateSpec,
			Strategy: appsapi.DeploymentStrategy{
				Type:          deployStrategy,
				RollingUpdate: rollingUpdate,
			},
		},
	}

	rawoverrides := gd.cr.Spec.UnsupportedConfigOverrides.Raw
	if len(rawoverrides) > 0 {
		var overrides ConfigOverrides
		if err := json.Unmarshal(rawoverrides, &overrides); err != nil {
			return nil, fmt.Errorf("invalid unsupportedConfigOverrides: %w", err)
		}

		depoverrides := overrides.Deployment
		if depoverrides != nil {
			deploy.Spec.Template.Spec.RuntimeClassName = depoverrides.RuntimeClassName
			for key, val := range depoverrides.Annotations {
				deploy.Annotations[key] = val
				deploy.Spec.Template.Annotations[key] = val
			}
		}
	}

	dgst, err := strategy.Checksum(deploy)
	if err != nil {
		return nil, err
	}
	deploy.ObjectMeta.Annotations[defaults.ChecksumOperatorAnnotation] = dgst

	return deploy, nil
}

func (gd *generatorDeployment) Get() (runtime.Object, error) {
	return gd.lister.Get(gd.GetName())
}

func (gd *generatorDeployment) Create() (runtime.Object, error) {
	exp, err := gd.expected()
	if err != nil {
		return nil, err
	}

	dep, _, err := resourceapply.ApplyDeployment(
		context.TODO(), gd.client, gd.eventRecorder, exp.(*appsapi.Deployment), -1,
	)
	if err != nil {
		return nil, err
	}

	gd.UpdateLastGeneration(dep.ObjectMeta.Generation)
	return dep, nil
}

func (gd *generatorDeployment) Update(o runtime.Object) (runtime.Object, bool, error) {
	exp, err := gd.expected()
	if err != nil {
		return o, false, err
	}

	original, expected := o.(*appsapi.Deployment), exp.(*appsapi.Deployment)

	// XXX if we are removing the affinities from the spec we need to do
	// that on its own api call so not to mess with scheduling logic.
	// in other words: already scheduled pods have those anti-affinities
	// in place and we may hit a scenario where new pods will conflict with
	// those anti-affinities.
	if original.Spec.Template.Spec.Affinity != nil && expected.Spec.Template.Spec.Affinity == nil {
		original = original.DeepCopy()
		original.Spec.Template.Spec.Affinity = nil
		dep, err := gd.client.Deployments(gd.GetNamespace()).Update(
			context.TODO(), original, metav1.UpdateOptions{},
		)
		if err != nil {
			return o, false, err
		}
		return dep, true, nil
	}

	dep, updated, err := resourceapply.ApplyDeployment(
		context.TODO(), gd.client, gd.eventRecorder, exp.(*appsapi.Deployment), gd.LastGeneration(),
	)
	if err != nil {
		return o, false, err
	}

	if updated {
		gd.UpdateLastGeneration(dep.ObjectMeta.Generation)
	}

	return dep, updated, nil
}

func (gd *generatorDeployment) UpdateLastGeneration(lastGen int64) {
	for i, gen := range gd.cr.Status.Generations {
		if gen.Name == gd.GetName() &&
			gen.Group == gd.GetGroup() &&
			gen.Resource == gd.GetResource() &&
			gen.Namespace == gd.GetNamespace() {

			gd.cr.Status.Generations[i].LastGeneration = lastGen
			return
		}
	}

	gd.cr.Status.Generations = append(
		gd.cr.Status.Generations,
		operatorv1.GenerationStatus{
			Name:           gd.GetName(),
			Group:          gd.GetGroup(),
			Resource:       gd.GetResource(),
			Namespace:      gd.GetNamespace(),
			LastGeneration: lastGen,
		},
	)
}

func (gd *generatorDeployment) LastGeneration() int64 {
	for _, gen := range gd.cr.Status.Generations {
		if gen.Name == gd.GetName() &&
			gen.Group == gd.GetGroup() &&
			gen.Resource == gd.GetResource() &&
			gen.Namespace == gd.GetNamespace() {

			return gen.LastGeneration
		}
	}
	return -1
}

func (gd *generatorDeployment) Delete(opts metav1.DeleteOptions) error {
	return gd.client.Deployments(gd.GetNamespace()).Delete(
		context.TODO(), gd.GetName(), opts,
	)
}

func (g *generatorDeployment) Owned() bool {
	return true
}
