package framework

import (
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
)

func MustEnsureDefaultExternalRegistryHostnameIsSet(t *testing.T, client *Clientset) {
	var cfg *configapiv1.Image
	var err error
	externalHosts := []string{}
	err = wait.Poll(1*time.Second, AsyncOperationTimeout, func() (bool, error) {
		var err error
		cfg, err = client.Images().Get("cluster", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("waiting for the image config resource: the resource does not exist")
			cfg = nil
			return false, nil
		} else if err != nil {
			return false, err
		}
		if cfg == nil {
			return false, nil
		}
		externalHosts = cfg.Status.ExternalRegistryHostnames

		for _, h := range externalHosts {
			if strings.HasPrefix(h, defaults.RouteName+"-"+defaults.ImageRegistryOperatorNamespace) {
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("cluster image config resource was not updated with default external registry hostname: %v, err: %v", externalHosts, err)
	}
}

func EnsureExternalRegistryHostnamesAreSet(t *testing.T, client *Clientset, wantedHostnames []string) {
	var cfg *configapiv1.Image
	var err error
	err = wait.Poll(1*time.Second, AsyncOperationTimeout, func() (bool, error) {
		var err error
		cfg, err = client.Images().Get("cluster", metav1.GetOptions{})
		if errors.IsNotFound(err) {
			t.Logf("waiting for the image config resource: the resource does not exist")
			cfg = nil
			return false, nil
		} else if err != nil {
			return false, err
		}
		if cfg == nil {
			return false, nil
		}

		for _, wh := range wantedHostnames {
			found := false
			for _, h := range cfg.Status.ExternalRegistryHostnames {
				if wh == h {
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		t.Errorf("cluster image config resource was not updated with external registry hostnames: wanted: %#v, got: %#v,  err: %v", wantedHostnames, cfg.Status.ExternalRegistryHostnames, err)
	}
}
