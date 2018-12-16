package util

import (
	"fmt"

	"github.com/golang/glog"

	operatorapi "github.com/openshift/api/operator/v1alpha1"
	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"
)

func UpdateCondition(cr *regopapi.ImageRegistry, conditionType string, status operatorapi.ConditionStatus, reason string, message string) {
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

func CreateOrUpdateSecret(name string, namespace string, data map[string]string) (*coreapi.Secret, error) {
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	var updatedSecret *coreapi.Secret

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur, err := client.Secrets(namespace).Get(name, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}

			glog.Warningf("secret %q not found: %s, creating", fmt.Sprintf("%s/%s", namespace, name), err)

			cur = &coreapi.Secret{
				ObjectMeta: metaapi.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
			}
		}

		if cur.StringData == nil {
			cur.StringData = make(map[string]string)
		}
		for k, v := range data {
			cur.StringData[k] = v
		}

		if errors.IsNotFound(err) {
			_, err := client.Secrets(namespace).Create(cur)
			return err
		}
		updatedSecret, err = client.Secrets(namespace).Update(cur)
		return err

	}); err != nil {
		return nil, err
	}

	return updatedSecret, err
}
