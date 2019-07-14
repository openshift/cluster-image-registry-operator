package listers

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
)

type MockConfigMapNamespaceLister struct {
	namespace string
	client    coreset.CoreV1Interface
}

func (m MockConfigMapNamespaceLister) Get(name string) (*corev1.ConfigMap, error) {
	return m.client.ConfigMaps(m.namespace).Get(name, metav1.GetOptions{})
}

func (m MockConfigMapNamespaceLister) List(selector labels.Selector) ([]*corev1.ConfigMap, error) {
	configMapList, err := m.client.ConfigMaps(m.namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var list []*corev1.ConfigMap
	for _, item := range configMapList.Items {
		list = append(list, &item)
	}
	return list, nil

}
