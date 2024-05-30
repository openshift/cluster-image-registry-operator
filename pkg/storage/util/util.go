package util

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

// ChunkSizeMiBFeatureGateName is a constant use in helper function for testing
const ChunkSizeMiBFeatureGateName = "ChunkSizeMiB"

// multiDashes is a regexp matching multiple dashes in a sequence.
var multiDashes = regexp.MustCompile(`-{2,}`)

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

// FetchCondition will return the provided condition.
func FetchCondition(cr *imageregistryv1.Config, conditionType string) (c operatorapi.OperatorCondition) {
	for _, c = range cr.Status.Conditions {
		if conditionType == c.Type {
			return c
		}
	}
	return
}

// GetInfrastructure gets information about the cloud platform that the cluster is
// installed on including the Type, Region, and other platform specific information.
func GetInfrastructure(lister configlisters.InfrastructureLister) (*configv1.Infrastructure, error) {
	return lister.Get("cluster")
}

// GetValueFromSecret gets value for key in a secret
// or returns an error if it does not exist
func GetValueFromSecret(sec *corev1.Secret, key string) (string, error) {
	if v, ok := sec.Data[key]; ok {
		return string(v), nil
	}
	return "", fmt.Errorf("secret %q does not contain required key %q", fmt.Sprintf("%s/%s", sec.Namespace, sec.Name), key)
}

// GenerateStorageName generates a unique name for the storage
// medium that the registry will use
func GenerateStorageName(listers *regopclient.StorageListers, additionalInfo ...string) (string, error) {
	// Get the infrastructure name
	infra, err := GetInfrastructure(listers.Infrastructures)
	if err != nil {
		return "", err
	}

	// A slice to store the parts of our name
	var parts []string

	// Put the infrastructure name first
	parts = append(parts, infra.Status.InfrastructureName)

	// Image Registry Name second
	parts = append(parts, defaults.ImageRegistryName)

	// Additional information provided to the function third
	for _, i := range additionalInfo {
		if len(i) != 0 {
			parts = append(parts, i)
		}
	}

	// Join the slice together with dashes, removing any occurrence of
	// multiple dashes in a row as some cloud providers consider this
	// invalid.
	name := multiDashes.ReplaceAllString(strings.Join(parts, "-"), "-")

	// Check the length and pad or truncate as needed
	switch {
	case len(name) < 62:
		padding := 62 - len(name) - 1
		bytes := make([]byte, padding)
		for i := 0; i < padding; i++ {
			bytes[i] = byte(97 + rand.Intn(25)) // a=97 and z=97+25
		}
		name = fmt.Sprintf("%s-%s", name, string(bytes))
	case len(name) > 62:
		name = name[0:62]
		if strings.HasSuffix(name, "-") {
			name = name[0:61] + string(byte(97+rand.Intn(25)))
		}
	}

	return strings.ToLower(name), nil
}
