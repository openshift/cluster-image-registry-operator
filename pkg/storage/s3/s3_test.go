package s3

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
)

func TestGetConfig(t *testing.T) {
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

	s3Driver := &driver{
		Listers: listers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageS3{},
	}

	config, err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageS3{
		Region: "us-east-1",
	}

	if !reflect.DeepEqual(config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, config))
	}
}

func TestGetConfigCustomRegionEndpoint(t *testing.T) {
	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "example",
					ServiceEndpoints: []configv1.AWSServiceEndpoint{
						{
							Name: "ec2",
							URL:  "https://ec2.example.com",
						},
						{
							Name: "s3",
							URL:  "https://s3.example.com",
						},
					},
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

	s3Driver := &driver{
		Listers: listers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageS3{},
	}
	config, err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageS3{
		Region:             "example",
		RegionEndpoint:     "https://s3.example.com",
		VirtualHostedStyle: true,
	}
	if !reflect.DeepEqual(config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, config))
	}
}

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

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                          "s3",
		"REGISTRY_STORAGE_S3_REGION":                "us-east-1",
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          true,
		"REGISTRY_STORAGE_S3_VIRTUALHOSTEDSTYLE":    false,
		"REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH": filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey),
	}
	for key, value := range expectedVars {
		e := findEnvVar(envvars, key)
		if e == nil {
			t.Fatalf("envvar %s not found, %v", key, envvars)
		}
		if e.Value != value {
			t.Errorf("%s: got %#+v, want %#+v", key, e.Value, value)
		}
	}
}

func TestServiceEndpointCanBeOverwritten(t *testing.T) {
	ctx := context.Background()

	config := &imageregistryv1.ImageRegistryConfigStorageS3{
		Region: "us-west-1",
	}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "hidden",
					ServiceEndpoints: []configv1.AWSServiceEndpoint{
						{
							Name: "s3",
							URL:  "https://s3.example.com",
						},
					},
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

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                          "s3",
		"REGISTRY_STORAGE_S3_REGION":                "us-west-1",
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          true,
		"REGISTRY_STORAGE_S3_VIRTUALHOSTEDSTYLE":    false,
		"REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH": filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey),
	}
	for key, value := range expectedVars {
		e := findEnvVar(envvars, key)
		if e == nil {
			t.Fatalf("envvar %s not found, %v", key, envvars)
		}
		if e.Value != value {
			t.Errorf("%s: got %#+v, want %#+v", key, e.Value, value)
		}
	}

	e := findEnvVar(envvars, "REGISTRY_STORAGE_S3_REGIONENDPOINT")
	if e != nil {
		t.Errorf("REGISTRY_STORAGE_S3_REGIONENDPOINT is expected to be unset, but got %v", e)
	}
}

type tripper struct {
	req           int
	responseCodes []int
}

func (r *tripper) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		r.req++
	}()

	code := http.StatusOK
	if r.req < len(r.responseCodes) {
		code = r.responseCodes[r.req]
	}

	return &http.Response{
		StatusCode: code,
		Body:       ioutil.NopCloser(bytes.NewBufferString("{}")),
	}, nil
}

func (r *tripper) AddResponse(code int) {
	r.responseCodes = append(r.responseCodes, code)
}

func TestStorageManagementState(t *testing.T) {
	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "us-west-1",
				},
			},
		},
	})
	builder.AddSecrets(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.CloudCredentialsName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Data: map[string][]byte{
			"aws_access_key_id":     []byte("access_key_id"),
			"aws_secret_access_key": []byte("secret_access_key"),
		},
	})
	listers := builder.BuildListers()

	for _, tt := range []struct {
		name                    string
		config                  *imageregistryv1.Config
		responseCodes           []int
		expectedManagementState string
	}{
		{
			name:                    "empty config",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
					},
				},
			},
		},
		{
			name:                    "empty config (management set)",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						S3:              &imageregistryv1.ImageRegistryConfigStorageS3{},
					},
				},
			},
		},
		{
			name:                    "existing bucket provided",
			expectedManagementState: imageregistryv1.StorageManagementStateUnmanaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "a-bucket",
						},
					},
				},
			},
		},
		{
			name:                    "existing bucket provided (management set)",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "another-bucket",
						},
					},
				},
			},
		},
		{
			name:                    "non-existing bucket provided",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "yet-another-bucket",
						},
					},
				},
			},
			responseCodes: []int{http.StatusNotFound},
		},
		{
			name:                    "non-existing bucket provided (management set)",
			expectedManagementState: "bar",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "bar",
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "another-bucket",
						},
					},
				},
			},
			responseCodes: []int{http.StatusNotFound},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rt := &tripper{}
			if len(tt.responseCodes) > 0 {
				for _, code := range tt.responseCodes {
					rt.AddResponse(code)
				}
			}

			drv := NewDriver(context.Background(), tt.config.Spec.Storage.S3, listers)

			drv.roundTripper = rt

			if err := drv.CreateStorage(tt.config); err != nil {
				t.Errorf("unexpected err %q", err)
				return
			}

			if tt.config.Spec.Storage.ManagementState != tt.expectedManagementState {
				t.Errorf(
					"expecting state to be %q, %q instead",
					tt.expectedManagementState,
					tt.config.Spec.Storage.ManagementState,
				)
			}
		})
	}
}
