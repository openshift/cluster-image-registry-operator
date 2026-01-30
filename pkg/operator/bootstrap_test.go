package operator

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
)

func TestBootstrapAWS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	setup := newTestControllerSetup(t)

	// Add config objects
	if err := setup.configClient.Tracker().Add(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
			},
		},
	}); err != nil {
		t.Fatalf("faile to add infrastructure to tracker: %v", err)
	}

	if err := setup.configClient.Tracker().Add(&configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}); err != nil {
		t.Fatalf("failed to add api server to tracker: %v", err)
	}

	// Start the controller
	setup.start(t, ctx)

	if err := setup.controller.Bootstrap(); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	config, err := setup.regClient.ImageregistryV1().Configs().Get(ctx, "cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Verify ObservedConfig is populated
	if len(config.Spec.ObservedConfig.Raw) == 0 {
		t.Error("expected ObservedConfig to be populated")
	}

	// Compare the rest of the spec (excluding ObservedConfig)
	expectedSpec := imageregistryv1.ImageRegistrySpec{
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
		},
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState:  "Managed",
			LogLevel:         operatorv1.Normal,
			OperatorLogLevel: operatorv1.Normal,
			ObservedConfig:   config.Spec.ObservedConfig,
		},
		Replicas:        2,
		RolloutStrategy: "RollingUpdate",
	}
	if !reflect.DeepEqual(config.Spec, expectedSpec) {
		t.Errorf("unexpected config: %s", cmp.Diff(expectedSpec, config.Spec))
	}
}
