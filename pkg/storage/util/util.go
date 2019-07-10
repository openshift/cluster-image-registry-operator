package util

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	configv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	installer "github.com/openshift/installer/pkg/types"
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

// GetInfrastructure gets information about the cloud platform that the cluster is
// installed on including the Type, Region, and other platform specific information.
// Currently the install config is used as a backup to be compatible with upgrades
// from 4.1 -> 4.2 when platformStatus did not exist, but should be able to be removed
// in the future.
func GetInfrastructure(listers *regopclient.Listers) (*configv1.Infrastructure, error) {
	infra, err := listers.Infrastructures.Get("cluster")
	if err != nil {
		return nil, err
	}

	if infra.Status.PlatformStatus == nil {
		infra.Status.PlatformStatus = &configv1.PlatformStatus{
			Type: infra.Status.Platform,
		}

		// TODO: Eventually we should be able to remove our dependency on the install config
		// but it is needed for now since platformStatus doesn't get set on upgrade
		// from 4.1 -> 4.2
		ic, err := listers.InstallerSecrets.Get("cluster-config-v1")
		if err != nil {
			return nil, err
		}
		installConfig := &installer.InstallConfig{}
		if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(ic.Data["install-config"])), 100).Decode(installConfig); err != nil {
			return nil, fmt.Errorf("unable to decode cluster install configuration: %v", err)
		}

		if installConfig.Platform.AWS != nil {
			infra.Status.PlatformStatus.AWS = &configv1.AWSPlatformStatus{Region: installConfig.Platform.AWS.Region}
		}
	}

	return infra, nil
}

// GetValueFromSecret gets value for key in a secret
// or returns an error if it does not exist
func GetValueFromSecret(sec *corev1.Secret, key string) (string, error) {
	if v, ok := sec.Data[key]; ok {
		return string(v), nil
	}
	return "", fmt.Errorf("secret %q does not contain required key %q", fmt.Sprintf("%s/%s", sec.Namespace, sec.Name), key)
}
