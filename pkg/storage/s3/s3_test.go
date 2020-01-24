package s3

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
)

func findEnvVar(envvars envvar.List, name string) *envvar.EnvVar {
	for i, e := range envvars {
		if e.Name == name {
			return &envvars[i]
		}
	}
	return nil
}

func TestConfigEnv(t *testing.T) {
	ctx := context.Background()

	config := &imageregistryv1.ImageRegistryConfigStorageS3{}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "us-east-1",
				},
			},
		},
	})
	testBuilder.AddSecrets(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.CloudCredentialsName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Data: map[string][]byte{
			"aws_access_key_id":     []byte("access"),
			"aws_secret_access_key": []byte("secret"),
		},
	})
	listers := testBuilder.BuildListers()

	d := NewDriver(ctx, config, listers)
	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	e := findEnvVar(envvars, "REGISTRY_STORAGE_S3_USEDUALSTACK")
	if e == nil {
		t.Fatalf("envvar REGISTRY_STORAGE_S3_USEDUALSTACK not found, %v", envvars)
	}
	if e.Value != true {
		t.Fatalf("USEDUALSTACK: got %#+v, want %#+v", e.Value, true)
	}
}
