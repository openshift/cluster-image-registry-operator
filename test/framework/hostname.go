package framework

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func EnsureDefaultExternalRegistryHostnameIsSet(te TestEnv) {
	var cfg *configapiv1.Image
	var err error
	externalHosts := []string{}
	err = wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			var err error
			cfg, err = te.Client().Images().Get(
				ctx, "cluster", metav1.GetOptions{},
			)
			if errors.IsNotFound(err) {
				te.Logf("waiting for the image config resource: the resource does not exist")
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
		},
	)
	if err != nil {
		te.Fatalf("cluster image config resource was not updated with default external registry hostname: %v, err: %v", externalHosts, err)
	}
}

func EnsureExternalRegistryHostnamesAreSet(te TestEnv, wantedHostnames []string) {
	var cfg *configapiv1.Image
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			var err error
			cfg, err = te.Client().Images().Get(
				ctx, "cluster", metav1.GetOptions{},
			)
			if errors.IsNotFound(err) {
				te.Logf("waiting for the image config resource: the resource does not exist")
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
		},
	)
	if err != nil {
		te.Errorf("cluster image config resource was not updated with external registry hostnames: wanted: %#v, got: %#v,  err: %v", wantedHostnames, cfg.Status.ExternalRegistryHostnames, err)
	}
}
