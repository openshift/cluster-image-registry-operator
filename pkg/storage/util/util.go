package util

import (
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

// UpdateCondition will update or add the provided condition.
func UpdateCondition(cr *imageregistryv1.Config, conditionType string, status operatorapi.ConditionStatus, reason string, message string) {
	found := false
	condition := &operatorapi.OperatorCondition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: metaapi.Now(),
	}
	conditions := []operatorapi.OperatorCondition{}

	for _, c := range cr.Status.Conditions {
		if condition.Type != c.Type {
			conditions = append(conditions, c)
			continue
		}
		if c.Status != condition.Status {
			c.Status = condition.Status
			c.LastTransitionTime = condition.LastTransitionTime
		}
		if c.Reason != condition.Reason {
			c.Reason = condition.Reason
		}
		if c.Message != condition.Message {
			c.Message = condition.Message
		}
		conditions = append(conditions, c)
		found = true
	}

	if !found {
		conditions = append(conditions, *condition)
	}

	cr.Status.Conditions = conditions
}

func GetClusterVersionConfig(kubeconfig *rest.Config) (*configv1.ClusterVersion, error) {
	client, err := configv1client.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return client.ConfigV1().ClusterVersions().Get("version", metav1.GetOptions{})
}

func GetInfrastructure(kubeconfig *rest.Config) (*configv1.Infrastructure, error) {
	client, err := configv1client.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	return client.ConfigV1().Infrastructures().Get("cluster", metav1.GetOptions{})
}
