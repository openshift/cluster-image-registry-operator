package gcs

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

// tripper is injected on gcs client to simulate api responses.
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
		Body:       ioutil.NopCloser(bytes.NewBufferString(r.responseBodies[r.req])),
	}, nil
}

func (r *tripper) AddResponse(code int, body string) {
	r.responseCodes = append(r.responseCodes, code)
	r.responseBodies = append(r.responseBodies, body)
}

func TestStorageManagementState(t *testing.T) {
	accountConfigJSON, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "project-id",
		"private_key_id": "key-id",
		"client_email":   "service-account-email",
		"client_id":      "client-id",
	})
	if err != nil {
		t.Fatalf("error marshalling config json: %v", err)
	}

	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.GCPPlatformType,
				GCP:  &configv1.GCPPlatformStatus{},
			},
		},
	})
	builder.AddSecrets(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.CloudCredentialsName,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Data: map[string][]byte{
			"service_account.json": accountConfigJSON,
		},
	})
	listers := builder.BuildListers()

	for _, tt := range []struct {
		name                    string
		config                  *imageregistryv1.Config
		expectedManagementState string
		responseCodes           []int
		responseBodies          []string
		err                     string
	}{
		{
			name:                    "bootstrap",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{},
					},
				},
			},
		},
		{
			name:                    "user manually set the bucket (bucket exists)",
			expectedManagementState: imageregistryv1.StorageManagementStateUnmanaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
							Bucket: "abucket",
						},
					},
				},
			},
		},
		{
			name:                    "user manually set the bucket (bucket doesn't exist)",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
							Bucket: "another-bucket",
						},
					},
				},
			},
			responseCodes:  []int{http.StatusNotFound, http.StatusOK},
			responseBodies: []string{`{"error":{"code":404}}`, `{}`},
		},
		{
			name: "unexpected api error (management state unset)",
			err:  "got HTTP response code 424 with body",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
							Bucket: "yet-another-bucket",
						},
					},
				},
			},
			responseCodes:  []int{http.StatusFailedDependency},
			responseBodies: []string{`<!--?`},
		},
		{
			name:                    "unexpected api error (management state set)",
			err:                     "got HTTP response code 424 with body",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
							Bucket: "bucket-the-return",
						},
					},
				},
			},
			responseCodes:  []int{http.StatusFailedDependency},
			responseBodies: []string{`<!--?`},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rt := &tripper{}
			if len(tt.responseCodes) == 0 {
				rt.AddResponse(http.StatusOK, "{}")
			} else {
				for i, code := range tt.responseCodes {
					rt.AddResponse(code, tt.responseBodies[i])
				}
			}

			drv := NewDriver(context.Background(), tt.config.Spec.Storage.GCS, nil, listers)
			drv.httpClient = &http.Client{Transport: rt}

			if err := drv.CreateStorage(tt.config); err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
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
