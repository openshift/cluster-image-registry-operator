package listers

import (
	"context"

	coreapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
)

type MockSecretNamespaceLister struct {
	namespace string
	client    coreset.CoreV1Interface
}

func (m MockSecretNamespaceLister) Get(name string) (*coreapi.Secret, error) {
	return m.client.Secrets(m.namespace).Get(
		context.Background(), name, metav1.GetOptions{},
	)
}

func (m MockSecretNamespaceLister) List(selector labels.Selector) ([]*coreapi.Secret, error) {
	secretList, err := m.client.Secrets(m.namespace).List(
		context.Background(), metav1.ListOptions{},
	)
	if err != nil {
		return nil, err
	}
	var list []*coreapi.Secret
	for _, item := range secretList.Items {
		list = append(list, &item)
	}
	return list, nil
}
