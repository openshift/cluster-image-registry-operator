package framework

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"

	openshiftapiv1 "github.com/openshift/api/config/v1"

	imageregistryapiv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
)

// SetResourceProxyConfig patches the image registry resource to contain the provided proxy configuration
func SetResourceProxyConfig(proxyConfig imageregistryapiv1.ImageRegistryConfigProxy, client *Clientset) error {
	_, err := client.Configs().Patch(imageregistryapiv1.ImageRegistryResourceName, types.MergePatchType, []byte(fmt.Sprintf(`{"spec": {"proxy": {"http": "%s", "https": "%s", "noProxy": "%s"}}}`, proxyConfig.HTTP, proxyConfig.HTTPS, proxyConfig.NoProxy)))

	return err
}

// ResetResourceProxyConfig patches the image registry resource to contain an empty proxy configuration
func ResetResourceProxyConfig(client *Clientset) error {
	_, err := client.Configs().Patch(imageregistryapiv1.ImageRegistryResourceName, types.MergePatchType, []byte(`{"spec": {"proxy": {"http": "", "https": "", "noProxy": ""}}}`))

	return err
}

// SetClusterProxyConfig patches the cluster proxy resource to contain the provided proxy configuration
func SetClusterProxyConfig(proxyConfig openshiftapiv1.ProxySpec, client *Clientset) error {
	proxy, err := client.Proxies().Patch(imageregistryapiv1.ClusterProxyResourceName, types.MergePatchType, []byte(fmt.Sprintf(`{"spec": {"httpProxy": "%s", "httpsProxy": "%s", "noProxy": "%s"}}`, proxyConfig.HTTPProxy, proxyConfig.HTTPSProxy, proxyConfig.NoProxy)))
	if err != nil {
		return err
	}

	// We can remove this once the proxy settings are properly
	// checked and the Status is updated
	proxy.Status.HTTPProxy = proxyConfig.HTTPProxy
	proxy.Status.HTTPSProxy = proxyConfig.HTTPSProxy
	proxy.Status.NoProxy = proxyConfig.NoProxy
	if _, err = client.Proxies().UpdateStatus(proxy); err != nil {
		return err
	}

	return nil
}

// SetClusterProxyConfig patches the cluster proxy resource to contain an empty proxy configuration
func ResetClusterProxyConfig(client *Clientset) error {
	proxy, err := client.Proxies().Patch(imageregistryapiv1.ClusterProxyResourceName, types.MergePatchType, []byte(`{"spec": {"httpProxy": "", "httpsProxy": "", "noProxy": ""}}`))
	if err != nil {
		return err
	}

	// We can remove this once the proxy settings are properly
	// checked and the Status is updated
	proxy.Status.HTTPProxy = ""
	proxy.Status.HTTPSProxy = ""
	proxy.Status.NoProxy = ""
	if _, err = client.Proxies().UpdateStatus(proxy); err != nil {
		return err
	}

	return nil

}
