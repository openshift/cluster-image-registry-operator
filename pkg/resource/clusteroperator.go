package resource

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	appsapi "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	appslisters "k8s.io/client-go/listers/apps/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	configv1helpers "github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func prefixConditions(conditions []operatorv1.OperatorCondition, prefix string) []operatorv1.OperatorCondition {
	out := make([]operatorv1.OperatorCondition, len(conditions))
	copy(out, conditions)
	for i := range out {
		out[i].Type = prefix + out[i].Type
	}
	return out
}

func unionStatus(normalStatus operatorv1.ConditionStatus, conditions []operatorv1.OperatorCondition) operatorv1.ConditionStatus {
	unknown := false
	for _, condition := range conditions {
		if condition.Status == operatorv1.ConditionUnknown {
			unknown = true
		} else if condition.Status != normalStatus {
			return condition.Status
		}
	}
	if unknown {
		return operatorv1.ConditionUnknown
	}
	return normalStatus
}

func latestTransitionTime(conditions []operatorv1.OperatorCondition) metav1.Time {
	latestTransitionTime := metav1.Time{}
	for _, condition := range conditions {
		if latestTransitionTime.Before(&condition.LastTransitionTime) {
			latestTransitionTime = condition.LastTransitionTime
		}
	}
	return latestTransitionTime
}

func unionReason(unionConditionType string, conditions []operatorv1.OperatorCondition) string {
	typeReasons := []string{}
	for _, condition := range conditions {
		if condition.Reason == "" || condition.Reason == "AsExpected" {
			continue
		}
		conditionType := condition.Type[:len(condition.Type)-len(unionConditionType)]
		typeReasons = append(typeReasons, conditionType+condition.Reason)
	}
	if len(typeReasons) == 0 {
		return ""
	}
	sort.Strings(typeReasons)
	return strings.Join(typeReasons, "::")
}

func unionMessage(conditions []operatorv1.OperatorCondition) string {
	messages := []string{}
	for _, condition := range conditions {
		if len(condition.Message) == 0 {
			continue
		}
		for _, message := range strings.Split(condition.Message, "\n") {
			messages = append(messages, fmt.Sprintf("%s: %s", condition.Type, message))
		}
	}
	return strings.Join(messages, "\n")
}

func unionCondition(unionConditionType string, normalStatus operatorv1.ConditionStatus, conditions []operatorv1.OperatorCondition) configv1.ClusterOperatorStatusCondition {
	var interestingConditions []operatorv1.OperatorCondition
	for _, condition := range conditions {
		if strings.HasSuffix(condition.Type, unionConditionType) {
			interestingConditions = append(interestingConditions, condition)
		}
	}

	unionCondition := configv1.ClusterOperatorStatusCondition{
		Type:               configv1.ClusterStatusConditionType(unionConditionType),
		Status:             configv1.ConditionStatus(unionStatus(normalStatus, interestingConditions)),
		LastTransitionTime: latestTransitionTime(interestingConditions),
		Reason:             unionReason(unionConditionType, interestingConditions),
		Message:            unionMessage(interestingConditions),
	}
	if unionCondition.Status == configv1.ConditionStatus(normalStatus) && unionCondition.Reason == "" {
		unionCondition.Reason = "AsExpected"
	}
	return unionCondition
}

var _ Mutator = &generatorClusterOperator{}

type generatorClusterOperator struct {
	relatedObjects []configv1.ObjectReference
	cr             *imageregistryv1.Config
	imagePruner    *imageregistryv1.ImagePruner
	deployLister   appslisters.DeploymentNamespaceLister
	configLister   configlisters.ClusterOperatorLister
	configClient   configv1client.ClusterOperatorsGetter
}

func NewGeneratorClusterOperator(
	deployLister appslisters.DeploymentNamespaceLister,
	configLister configlisters.ClusterOperatorLister,
	configClient configv1client.ClusterOperatorsGetter,
	cr *imageregistryv1.Config,
	imagePruner *imageregistryv1.ImagePruner,
	relatedObjects []configv1.ObjectReference,
) *generatorClusterOperator {
	return &generatorClusterOperator{
		deployLister:   deployLister,
		configLister:   configLister,
		configClient:   configClient,
		cr:             cr,
		imagePruner:    imagePruner,
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

func (gco *generatorClusterOperator) syncConditions(op *configv1.ClusterOperator) (modified bool) {
	conditions := gco.cr.Status.Conditions
	if gco.imagePruner != nil {
		conditions = append(conditions, prefixConditions(gco.imagePruner.Status.Conditions, "ImagePruner")...)
	}

	oldStatus := op.Status.DeepCopy()
	configv1helpers.SetStatusCondition(&op.Status.Conditions, unionCondition("Available", operatorv1.ConditionTrue, conditions), nil)
	configv1helpers.SetStatusCondition(&op.Status.Conditions, unionCondition("Progressing", operatorv1.ConditionFalse, conditions), nil)
	configv1helpers.SetStatusCondition(&op.Status.Conditions, unionCondition("Degraded", operatorv1.ConditionFalse, conditions), nil)
	return !equality.Semantic.DeepEqual(oldStatus, &op.Status)
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
