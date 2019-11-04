package resource

import (
	"fmt"
	"reflect"

	appsapi "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog"

	configapi "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

var _ Mutator = &generatorClusterOperator{}

type generatorClusterOperator struct {
	mutators     []Mutator
	cr           *imageregistryv1.Config
	deployLister appslisters.DeploymentNamespaceLister
	configLister configlisters.ClusterOperatorLister
	configClient configset.ConfigV1Interface
}

func newGeneratorClusterOperator(deployLister appslisters.DeploymentNamespaceLister, configLister configlisters.ClusterOperatorLister, configClient configset.ConfigV1Interface, cr *imageregistryv1.Config, mutators []Mutator) *generatorClusterOperator {
	return &generatorClusterOperator{
		mutators:     mutators,
		cr:           cr,
		deployLister: deployLister,
		configLister: configLister,
		configClient: configClient,
	}
}

func (gco *generatorClusterOperator) Type() runtime.Object {
	return &configapi.ClusterOperator{}
}

func (gco *generatorClusterOperator) GetGroup() string {
	return configapi.GroupName
}

func (gco *generatorClusterOperator) GetResource() string {
	return "clusteroperators"
}

func (gco *generatorClusterOperator) GetNamespace() string {
	return ""
}

func (gco *generatorClusterOperator) GetName() string {
	return imageregistryv1.ImageRegistryClusterOperatorResourceName
}

func (gco *generatorClusterOperator) Get() (runtime.Object, error) {
	return gco.configLister.Get(gco.GetName())
}

func (gco *generatorClusterOperator) Create() (runtime.Object, error) {
	co := &configapi.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: gco.GetName(),
		},
	}

	_, err := gco.syncVersions(co)
	if err != nil {
		return co, err
	}

	_ = gco.syncConditions(co)
	_ = gco.syncRelatedObjects(co)

	return gco.configClient.ClusterOperators().Create(co)
}

func (gco *generatorClusterOperator) Update(o runtime.Object) (runtime.Object, bool, error) {
	co := o.(*configapi.ClusterOperator)

	modified, err := gco.syncVersions(co)
	if err != nil {
		return o, false, err
	}

	if gco.syncConditions(co) {
		modified = true
	}

	if gco.syncRelatedObjects(co) {
		modified = true
	}

	if !modified {
		return o, false, nil
	}

	n, err := gco.configClient.ClusterOperators().UpdateStatus(co)
	return n, err == nil, err
}

func (gco *generatorClusterOperator) Delete(opts *metav1.DeleteOptions) error {
	return gco.configClient.Images().Delete(gco.GetName(), opts)
}

func (gco *generatorClusterOperator) Owned() bool {
	// the registry operator can create and contribute to the clusteroperator, but it doesn't own it.
	return false
}

func convertOperatorStatus(status operatorapi.ConditionStatus) (configapi.ConditionStatus, error) {
	switch status {
	case operatorapi.ConditionTrue:
		return configapi.ConditionTrue, nil
	case operatorapi.ConditionFalse:
		return configapi.ConditionFalse, nil
	case operatorapi.ConditionUnknown:
		return configapi.ConditionUnknown, nil
	}
	return configapi.ConditionUnknown, fmt.Errorf("unexpected condition status: %s", status)
}

func (gco *generatorClusterOperator) syncConditions(op *configapi.ClusterOperator) (modified bool) {
	conditions := []configapi.ClusterOperatorStatusCondition{}

	for _, resourceCondition := range gco.cr.Status.Conditions {
		found := false

		var conditionType configapi.ClusterStatusConditionType

		switch resourceCondition.Type {
		case operatorapi.OperatorStatusTypeAvailable:
			conditionType = configapi.OperatorAvailable
		case operatorapi.OperatorStatusTypeProgressing:
			conditionType = configapi.OperatorProgressing
		case operatorapi.OperatorStatusTypeDegraded:
			conditionType = configapi.OperatorDegraded
		default:
			continue
		}

		for i, clusterOperatorCondition := range op.Status.Conditions {
			if conditionType != clusterOperatorCondition.Type {
				continue
			}
			found = true

			newStatus, err := convertOperatorStatus(resourceCondition.Status)
			if err != nil {
				klog.Errorf("ignore condition of %s custom resource: %s", gco.cr.Name, err)
				continue
			}

			if clusterOperatorCondition.Status != newStatus {
				op.Status.Conditions[i].Status = newStatus
				modified = true
			}

			if op.Status.Conditions[i].LastTransitionTime != resourceCondition.LastTransitionTime {
				op.Status.Conditions[i].LastTransitionTime = resourceCondition.LastTransitionTime
				modified = true
			}

			if op.Status.Conditions[i].Reason != resourceCondition.Reason {
				op.Status.Conditions[i].Reason = resourceCondition.Reason
				modified = true
			}

			if op.Status.Conditions[i].Message != resourceCondition.Message {
				op.Status.Conditions[i].Message = resourceCondition.Message
				modified = true
			}
		}

		if !found {
			conditionStatus, err := convertOperatorStatus(resourceCondition.Status)
			if err != nil {
				klog.Errorf("ignore condition of %s custom resource: %s", gco.cr.Name, err)
				continue
			}
			conditions = append(conditions, configapi.ClusterOperatorStatusCondition{
				Type:               conditionType,
				Status:             conditionStatus,
				LastTransitionTime: resourceCondition.LastTransitionTime,
				Reason:             resourceCondition.Reason,
				Message:            resourceCondition.Message,
			})
			modified = true
		}
	}

	for i := range conditions {
		op.Status.Conditions = append(op.Status.Conditions, conditions[i])
	}

	return
}

// isDeploymentStatusAvailableAndUpdated returns true when at least one
// replica instance exists and all replica instances are current,
// there are no replica instances remaining from the previous deployment.
// There may still be additional replica instances being created.
func isDeploymentStatusAvailableAndUpdated(deploy *appsapi.Deployment) bool {
	return deploy.Status.AvailableReplicas > 0 &&
		deploy.Status.ObservedGeneration >= deploy.Generation &&
		deploy.Status.UpdatedReplicas == deploy.Status.Replicas
}

func (gco *generatorClusterOperator) syncVersions(op *configapi.ClusterOperator) (modified bool, err error) {
	deploy, err := gco.deployLister.Get(imageregistryv1.ImageRegistryName)
	if err != nil {
		if kerrors.IsNotFound(err) {
			err = nil
		}
		return
	}

	deploymentVersion := deploy.Annotations[imageregistryv1.VersionAnnotation]
	if len(deploymentVersion) == 0 || !isDeploymentStatusAvailableAndUpdated(deploy) {
		return
	}

	newVersions := []configapi.OperandVersion{
		{
			Name:    "operator",
			Version: deploymentVersion,
		},
	}

	if !reflect.DeepEqual(op.Status.Versions, newVersions) {
		op.Status.Versions = newVersions
		modified = true
	}

	return
}

func (gco *generatorClusterOperator) syncRelatedObjects(op *configapi.ClusterOperator) (modified bool) {
	var relatedObjects []configapi.ObjectReference

	// Always sync the openshift-image-registry namespace
	relatedObjects = append(relatedObjects, configapi.ObjectReference{
		Resource: "namespaces",
		Name:     imageregistryv1.ImageRegistryOperatorNamespace,
	})

	for _, gen := range gco.mutators {
		relatedObjects = append(relatedObjects, configapi.ObjectReference{
			Group:     gen.GetGroup(),
			Resource:  gen.GetResource(),
			Namespace: gen.GetNamespace(),
			Name:      gen.GetName(),
		})
	}

	if !reflect.DeepEqual(op.Status.RelatedObjects, relatedObjects) {
		op.Status.RelatedObjects = relatedObjects
		modified = true
	}

	return
}
