package framework

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	configapiv1 "github.com/openshift/api/config/v1"

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

type InspectedProxyConfig struct {
	*configapiv1.Proxy
}

func MustInspectProxyConfig(t *testing.T, client *Clientset) InspectedProxyConfig {
	proxy, err := client.Proxies().Get(imageregistryapiv1.ClusterProxyResourceName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return InspectedProxyConfig{Proxy: nil}
	} else if err != nil {
		t.Fatalf("unable to get the cluster proxy configuration: %s", err)
	}
	return InspectedProxyConfig{Proxy: proxy}
}

func (c InspectedProxyConfig) Restore(client *Clientset) error {
	if c.Proxy == nil {
		err := DeleteClusterProxyConfig(client)
		if err != nil {
			return fmt.Errorf("unable to restore (delete) the cluster proxy configuration: %s", err)
		}
	} else {
		err := SetClusterProxyConfig(client, c.Proxy.Spec)
		if err != nil {
			return fmt.Errorf("unable to restore the cluster proxy configuration: %s", err)
		}
	}
	return nil
}

func (c InspectedProxyConfig) RestoreOrDie(client *Clientset) {
	if err := c.Restore(client); err != nil {
		panic(err)
	}
}

// SetClusterProxyConfig patches the cluster proxy resource to contain the provided proxy configuration.
func SetClusterProxyConfig(client *Clientset, spec configapiv1.ProxySpec) error {
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		proxy, err := client.Proxies().Get(imageregistryapiv1.ClusterProxyResourceName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			proxy, err = client.Proxies().Create(&configapiv1.Proxy{
				Spec: spec,
			})
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			proxy.Spec = spec
			proxy, err = client.Proxies().Update(proxy)
			if err != nil {
				return err
			}
		}

		// FIXME: We can remove this once the proxy settings are properly
		// checked and the Status is updated
		proxy.Status.HTTPProxy = proxy.Spec.HTTPProxy
		proxy.Status.HTTPSProxy = proxy.Spec.HTTPSProxy
		proxy.Status.NoProxy = proxy.Spec.NoProxy
		if _, err := client.Proxies().UpdateStatus(proxy); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return fmt.Errorf("unable to set the cluster proxy configuration: %s", err)
	}
	return nil
}

// DeleteClusterProxyConfig deleted the cluster proxy resource.
func DeleteClusterProxyConfig(client *Clientset) error {
	err := client.Proxies().Delete(imageregistryapiv1.ClusterProxyResourceName, &metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("unable to delete the cluster proxy configuration: %s", err)
	}
	return nil
}
