package framework

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
)

func PlatformIsOneOf(te TestEnv, platforms []configv1.PlatformType) bool {
	infrastructureConfig, err := te.Client().Infrastructures().Get(
		context.Background(), "cluster", metav1.GetOptions{},
	)
	if err != nil {
		te.Fatalf("unable to get infrastructure object: %v", err)
	}

	typ := infrastructureConfig.Status.PlatformStatus.Type
	for _, p := range platforms {
		if p == typ {
			return true
		}
	}
	return false
}
