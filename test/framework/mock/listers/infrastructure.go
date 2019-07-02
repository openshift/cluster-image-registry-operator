package listers

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	configv1 "github.com/openshift/api/config/v1"
	configset "github.com/openshift/client-go/config/clientset/versioned"
)

type MockInfrastructureLister struct {
	client configset.Clientset
}

func (m MockInfrastructureLister) Get(name string) (*configv1.Infrastructure, error) {
	return m.client.ConfigV1().Infrastructures().Get(name, metav1.GetOptions{})
}

func (m MockInfrastructureLister) List(selector labels.Selector) ([]*configv1.Infrastructure, error) {
	infrastructureList, err := m.client.ConfigV1().Infrastructures().List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var list []*configv1.Infrastructure
	for _, item := range infrastructureList.Items {
		list = append(list, &item)
	}
	return list, nil
}
