package framework

import (
	"fmt"
	"testing"
	"time"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func MustEnsureServiceCAConfigMap(t *testing.T, client *Clientset) {
	expectedAnnotations := map[string]string{
		"service.beta.openshift.io/inject-cabundle": "true",
	}
	err := ensureConfigMap("serviceca", expectedAnnotations, client)
	if err != nil {
		t.Fatal(err)
	}
}

func ensureConfigMap(name string, annotations map[string]string, client *Clientset) error {
	var configMap *corev1.ConfigMap
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		configMap, err = client.ConfigMaps(defaults.ImageRegistryOperatorNamespace).Get(name, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
	if err != nil {
		return err
	}
	for k, expected := range annotations {
		actual, ok := configMap.Annotations[k]
		if !ok {
			return fmt.Errorf("expected annotation %s was not found on ConfigMap %s/%s", k, defaults.ImageRegistryOperatorNamespace, name)
		}
		if expected != actual {
			return fmt.Errorf("expected annotation %s on ConfigMap %s/%s to have value %s, got %s", k, defaults.ImageRegistryOperatorNamespace, name, expected, actual)
		}
	}
	return nil
}
