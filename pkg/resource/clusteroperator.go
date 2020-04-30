package resource

import (
	"context"
	"fmt"
	"os"
	"reflect"

	appsapi "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appslisters "k8s.io/client-go/listers/apps/v1"
	"k8s.io/klog"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

var _ Mutator = &generatorClusterOperator{}

type generatorClusterOperator struct {
	relatedObjects []configv1.ObjectReference
	cr             *imageregistryv1.Config
	deployLister   appslisters.DeploymentNamespaceLister
	configLister   configlisters.ClusterOperatorLister
	configClient   configv1client.ClusterOperatorsGetter
}

func NewGeneratorClusterOperator(
	deployLister appslisters.DeploymentNamespaceLister,
	configLister configlisters.ClusterOperatorLister,
	configClient configv1client.ClusterOperatorsGetter,
	cr *imageregistryv1.Config,
	relatedObjects []configv1.ObjectReference,
) *generatorClusterOperator {
	return &generatorClusterOperator{
		deployLister:   deployLister,
		configLister:   configLister,
		configClient:   configClient,
		cr:             cr,
		relatedObjects: relatedObjects,
	}
}

func (gco *generatorClusterOperator) Type() runtime.Object {
	return &configv1.ClusterOperator{}
}

func (gco *generatorClusterOperator) GetNamespace() string {
	return ""
}

func (gco *generatorClusterOperator) GetName() string {
	return defaults.ImageRegistryClusterOperatorResourceName
}

func (gco *generatorClusterOperator) Get() (runtime.Object, error) {
	return gco.configLister.Get(gco.GetName())
}

func (gco *generatorClusterOperator) Create() (runtime.Object, error) {
	co := &configv1.ClusterOperator{
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

	return gco.configClient.ClusterOperators().Create(
		context.TODO(), co, metav1.CreateOptions{},
	)
}

func (gco *generatorClusterOperator) Update(o runtime.Object) (runtime.Object, bool, error) {
	co := o.(*configv1.ClusterOperator)

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

	n, err := gco.configClient.ClusterOperators().UpdateStatus(
		context.TODO(), co, metav1.UpdateOptions{},
	)
	return n, err == nil, err
}

func (gco *generatorClusterOperator) Delete(opts metav1.DeleteOptions) error {
	return gco.configClient.ClusterOperators().Delete(
		context.TODO(), gco.GetName(), opts,
	)
}

func (gco *generatorClusterOperator) Owned() bool {
	// the registry operator can create and contribute to the clusteroperator, but it doesn't own it.
	return false
}

func convertOperatorStatus(status operatorapi.ConditionStatus) (configv1.ConditionStatus, error) {
	switch status {
	case operatorapi.ConditionTrue:
		return configv1.ConditionTrue, nil
	case operatorapi.ConditionFalse:
		return configv1.ConditionFalse, nil
	case operatorapi.ConditionUnknown:
		return configv1.ConditionUnknown, nil
	}
	return configv1.ConditionUnknown, fmt.Errorf("unexpected condition status: %s", status)
}

func (gco *generatorClusterOperator) syncConditions(op *configv1.ClusterOperator) (modified bool) {
	conditions := []configv1.ClusterOperatorStatusCondition{}

	for _, resourceCondition := range gco.cr.Status.Conditions {
		found := false

		var conditionType configv1.ClusterStatusConditionType

		switch resourceCondition.Type {
		case operatorapi.OperatorStatusTypeAvailable:
			conditionType = configv1.OperatorAvailable
		case operatorapi.OperatorStatusTypeProgressing:
			conditionType = configv1.OperatorProgressing
		case operatorapi.OperatorStatusTypeDegraded:
			conditionType = configv1.OperatorDegraded
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
			conditions = append(conditions, configv1.ClusterOperatorStatusCondition{
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

// syncVersions updates reported version.
//
// If in "Managed" state we use the version stored as a annotation on registry'
// Deployment, if not we use RELEASE_VERSION environment variable.
func (gco *generatorClusterOperator) syncVersions(op *configv1.ClusterOperator) (bool, error) {
	if gco.cr == nil {
		return false, fmt.Errorf("invalid nil configuration provided")
	}

	version := os.Getenv("RELEASE_VERSION")
	if gco.cr.Spec.ManagementState == operatorapi.Managed {
		deploy, err := gco.deployLister.Get(defaults.ImageRegistryName)
		if err != nil {
			if kerrors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		if !isDeploymentStatusAvailableAndUpdated(deploy) {
			return false, nil
		}

		version = deploy.Annotations[defaults.VersionAnnotation]
	}

	if len(version) == 0 {
		return false, nil
	}

	newVersions := []configv1.OperandVersion{
		{
			Name:    "operator",
			Version: version,
		},
	}

	if reflect.DeepEqual(op.Status.Versions, newVersions) {
		return false, nil
	}

	op.Status.Versions = newVersions
	return true, nil
}

func (gco *generatorClusterOperator) syncRelatedObjects(op *configv1.ClusterOperator) (modified bool) {
	if !reflect.DeepEqual(op.Status.RelatedObjects, gco.relatedObjects) {
		op.Status.RelatedObjects = gco.relatedObjects
		modified = true
	}

	return
}
