package generate

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

func GetConfigState(namespace string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry-config-state",
			Namespace: namespace,
		},
		Data: make(map[string]string),
	}

	err := sdk.Get(cm)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("failed to get current state: %s", err)
		}
	}
	return cm, nil
}

func SetConfigState(cr *v1alpha1.ImageRegistry, cm *corev1.ConfigMap) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				APIVersion: corev1.SchemeGroupVersion.String(),
				Kind:       "ConfigMap",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      cm.ObjectMeta.Name,
				Namespace: cm.ObjectMeta.Namespace,
			},
		}
		err := sdk.Get(cur)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get current state: %s", err)
			}
			addOwnerRefToObject(cm, asOwner(cr))
			return sdk.Create(cm)
		}
		cur.Data = cm.Data
		return sdk.Update(cur)
	})
}
