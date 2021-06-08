package ibmcos

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/resourcecontrollerv2"
	"github.com/IBM/platform-services-go-sdk/resourcemanagerv2"
	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
)

func TestConfigEnv(t *testing.T) {
	ctx := context.Background()
	config := &imageregistryv1.ImageRegistryConfigStorageIBMCOS{}
	testBuilder := cirofake.NewFixturesBuilder()

	// Mock Infrastructure
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.IBMCloudPlatformType,
				IBMCloud: &configv1.IBMCloudPlatformStatus{
					Location: "us-east",
				},
			},
		},
	})

	// Create test lister
	listers := testBuilder.BuildListers()

	// Create new driver for test
	d := NewDriver(ctx, config, listers)

	// Invoke ConfigEnv()
	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	// Expected values
	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                          "s3",
		"REGISTRY_STORAGE_S3_REGION":                "us-east",
		"REGISTRY_STORAGE_S3_REGIONENDPOINT":        "s3.us-east.cloud-object-storage.appdomain.cloud",
		"REGISTRY_STORAGE_S3_ENCRYPT":               false,
		"REGISTRY_STORAGE_S3_VIRTUALHOSTEDSTYLE":    false,
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          false,
		"REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH": filepath.Join(imageRegistrySecretMountpoint, imageRegistrySecretDataKey),
	}

	// Check values
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

func findEnvVar(envvars envvar.List, name string) *envvar.EnvVar {
	for i, e := range envvars {
		if e.Name == name {
			return &envvars[i]
		}
	}
	return nil
}

func TestStorageManagementState(t *testing.T) {
	ctx := context.Background()
	testBuilder := cirofake.NewFixturesBuilder()

	// Mock Infrastructure
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.IBMCloudPlatformType,
				IBMCloud: &configv1.IBMCloudPlatformStatus{
					Location:          "us-east",
					ResourceGroupName: "rg-test",
				},
			},
		},
	})

	// Mock Secret
	testBuilder.AddSecrets(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.CloudCredentialsName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Data: map[string][]byte{
			"ibmcloud_api_key": []byte("test-api-key"),
		},
	})

	// Create test lister
	listers := testBuilder.BuildListers()

	// Iterate management states
	for _, tt := range []struct {
		name                    string
		config                  *imageregistryv1.Config
		responseCodes           []int
		responseBodies          []string
		expectedManagementState string
	}{
		{
			name:                    "empty config",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						IBMCOS: &imageregistryv1.ImageRegistryConfigStorageIBMCOS{},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"resources": [{ "id": "rg-test-id"}]}`,
				`{"resources": []}`,
				`{"crn": "crn:test:instance:0"}`,
				`{"crn": "crn:test:resource-key:0"}`,
				`{}`,
				`{}`,
			},
		},
		{
			name:                    "empty config (management set)",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						IBMCOS:          &imageregistryv1.ImageRegistryConfigStorageIBMCOS{},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"resources": [{ "id": "rg-test-id"}]}`,
				`{"resources": []}`,
				`{"crn": "crn:test:instance:1"}`,
				`{"crn": "crn:test:resource-key:1"}`,
				`{}`,
				`{}`,
			},
		},
		{
			name:                    "existing service instance provided",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						IBMCOS: &imageregistryv1.ImageRegistryConfigStorageIBMCOS{
							ServiceInstanceCRN: "crn:test:instance",
						},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"crn": "crn:test:instance:2", "resource_group_id": "rg-test-id", "state": "active"}`,
				`{"name": "rg-test"}]}`,
				`{"crn": "crn:test:resource-key:2"}`,
				`{}`,
				`{}`,
			},
		},
		{
			name:                    "existing service instance and bucket provided",
			expectedManagementState: imageregistryv1.StorageManagementStateUnmanaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						IBMCOS: &imageregistryv1.ImageRegistryConfigStorageIBMCOS{
							Bucket:             "test-bucket",
							ServiceInstanceCRN: "crn:test:instance",
						},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"crn": "crn:test:instance:3", "resource_group_id": "rg-test-id", "state": "active"}`,
				`{"name": "rg-test"}]}`,
				`{"crn": "crn:test:resource-key:3"}`,
				`{}`,
			},
		},
		{
			name:                    "non-existing service instance provided",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						IBMCOS: &imageregistryv1.ImageRegistryConfigStorageIBMCOS{
							Bucket:             "test-bucket",
							ServiceInstanceCRN: "crn:test:instance:does-not-exist",
						},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"state": "removed"}`,
				`{"resources": [{ "id": "rg-test-id"}]}`,
				`{"resources": []}`,
				`{"crn": "crn:test:instance:4"}`,
				`{"crn": "crn:test:resource-key:4"}`,
				`{}`,
				`{}`,
			},
		},
		{
			name:                    "existing service instance and non-existing bucket provided",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						IBMCOS: &imageregistryv1.ImageRegistryConfigStorageIBMCOS{
							Bucket:             "test-bucket-does-not-exist",
							ServiceInstanceCRN: "crn:test:instance",
						},
					},
				},
			},
			responseCodes: []int{
				http.StatusOK,
				http.StatusOK,
				http.StatusOK,
				http.StatusNotFound,
				http.StatusOK,
				http.StatusOK,
			},
			responseBodies: []string{
				`{"crn": "crn:test:instance:5", "resource_group_id": "rg-test-id", "state": "active"}`,
				`{"name": "rg-test"}]}`,
				`{"crn": "crn:test:resource-key:5"}`,
				`{}`,
				`{}`,
				`{}`,
			},
		},
	} {
		// Test for each management state
		t.Run(tt.name, func(t *testing.T) {
			rt := &tripper{}
			if len(tt.responseCodes) == 0 {
				rt.AddResponse(http.StatusOK, "{}")
			} else {
				for i, code := range tt.responseCodes {
					rt.AddResponse(code, tt.responseBodies[i])
				}
			}

			drv := NewDriver(ctx, tt.config.Spec.Storage.IBMCOS, listers)
			drv.roundTripper = rt
			drv.resourceController = &resourcecontrollerv2.ResourceControllerV2{
				Service: &core.BaseService{
					Client: &http.Client{Transport: rt},
					Options: &core.ServiceOptions{
						URL:           "http://nowhere.cloud",
						Authenticator: &core.NoAuthAuthenticator{},
					},
				},
			}
			drv.resourceManager = &resourcemanagerv2.ResourceManagerV2{
				Service: &core.BaseService{
					Client: &http.Client{Transport: rt},
					Options: &core.ServiceOptions{
						URL:           "http://nowhere.cloud",
						Authenticator: &core.NoAuthAuthenticator{},
					},
				},
			}

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

type tripper struct {
	req            int
	responseCodes  []int
	responseBodies []string
}

func (r *tripper) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		r.req++
	}()

	return &http.Response{
		StatusCode: r.responseCodes[r.req],
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       ioutil.NopCloser(bytes.NewBufferString(r.responseBodies[r.req])),
	}, nil
}

func (r *tripper) AddResponse(code int, body string) {
	r.responseCodes = append(r.responseCodes, code)
	r.responseBodies = append(r.responseBodies, body)
}
