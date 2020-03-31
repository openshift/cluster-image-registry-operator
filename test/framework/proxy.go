package framework

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	openshiftapiv1 "github.com/openshift/api/config/v1"
	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

// SetResourceProxyConfig patches the image registry resource to contain the provided proxy configuration
func SetResourceProxyConfig(te TestEnv, proxyConfig imageregistryapiv1.ImageRegistryConfigProxy) {
	_, err := te.Client().Configs().Patch(defaults.ImageRegistryResourceName, types.MergePatchType, []byte(fmt.Sprintf(`{"spec": {"proxy": {"http": "%s", "https": "%s", "noProxy": "%s"}}}`, proxyConfig.HTTP, proxyConfig.HTTPS, proxyConfig.NoProxy)))
	if err != nil {
		te.Fatalf("unable to set resource proxy configuration: %v", err)
	}
}

// ResetResourceProxyConfig patches the image registry resource to contain an empty proxy configuration
func ResetResourceProxyConfig(te TestEnv) {
	_, err := te.Client().Configs().Patch(defaults.ImageRegistryResourceName, types.MergePatchType, []byte(`{"spec": {"proxy": {"http": "", "https": "", "noProxy": ""}}}`))
	if err != nil {
		te.Fatal(err)
	}
}

// SetClusterProxyConfig patches the cluster proxy resource to contain the provided proxy configuration
func SetClusterProxyConfig(te TestEnv, proxyConfig openshiftapiv1.ProxySpec) {
	_, err := te.Client().Proxies().Patch(defaults.ClusterProxyResourceName, types.MergePatchType, []byte(fmt.Sprintf(`{"spec": {"httpProxy": "%s", "httpsProxy": "%s", "noProxy": "%s"}}`, proxyConfig.HTTPProxy, proxyConfig.HTTPSProxy, proxyConfig.NoProxy)))
	if err != nil {
		te.Fatalf("unable to patch cluster proxy instance: %v", err)
	}
}

// ResetClusterProxyConfig patches the cluster proxy resource to contain an empty proxy configuration
func ResetClusterProxyConfig(te TestEnv) {
	_, err := te.Client().Proxies().Patch(defaults.ClusterProxyResourceName, types.MergePatchType, []byte(`{"spec": {"httpProxy": "", "httpsProxy": "", "noProxy": ""}}`))
	if err != nil {
		te.Fatal(err)
	}
}

// DumpClusterProxyResource prints out the cluster proxy configuration
func DumpClusterProxyResource(logger Logger, client *Clientset) {
	cr, err := client.Proxies().Get(defaults.ClusterProxyResourceName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the cluster proxy resource: %s", err)
		return
	}
	DumpYAML(logger, "the cluster proxy resource", cr)
}
