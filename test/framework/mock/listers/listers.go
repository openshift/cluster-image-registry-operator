package listers

import (
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"

	configset "github.com/openshift/client-go/config/clientset/versioned"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

const (
	installerConfigNamespace = "kube-system"
)

type mockLister struct {
	listers    regopclient.Listers
	kubeconfig *restclient.Config
}

func NewMockLister(kubeconfig *restclient.Config) (*mockLister, error) {
	return &mockLister{kubeconfig: kubeconfig}, nil
}

func (m *mockLister) GetListers() (*regopclient.Listers, error) {
	coreClient, err := coreset.NewForConfig(m.kubeconfig)
	if err != nil {
		return nil, err
	}

	configClient, err := configset.NewForConfig(m.kubeconfig)
	if err != nil {
		return nil, err
	}

	m.listers.Secrets = MockSecretNamespaceLister{namespace: defaults.ImageRegistryOperatorNamespace, client: coreClient}
	m.listers.InstallerConfigMaps = MockConfigMapNamespaceLister{namespace: installerConfigNamespace, client: coreClient}
	m.listers.Infrastructures = MockInfrastructureLister{client: *configClient}
	m.listers.OpenShiftConfigManaged = MockConfigMapNamespaceLister{namespace: defaults.OpenShiftConfigManagedNamespace, client: coreClient}

	return &m.listers, err
}
