package oss

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/google/go-cmp/cmp"
	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/stretchr/testify/assert"
)

var TestAccessKeyId = "TEST_ACCESS_KEY_ID"
var TestAccessKeySecret = "TEST_ACCESS_KEY_SECRET"
var TestBucketName = "a-bucket"
var TestAnotherBucketName = "another-bucket"

func TestGetConfig(t *testing.T) {
	testBuilder := cirofake.NewFixturesBuilder()
	region := "cn-beijing"

	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
					Region: region,
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := testBuilder.BuildListers()

	ossDriver := &driver{
		Listers: &listers.StorageListers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
	}

	err := ossDriver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
		Region: region,
	}

	if !reflect.DeepEqual(ossDriver.Config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, ossDriver.Config))
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
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
					Region: "cn-beijing",
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := testBuilder.BuildListers()

	ossDriver := &driver{
		Listers: &listers.StorageListers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
	}
	err := ossDriver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
		Region: "cn-beijing",
	}
	if !reflect.DeepEqual(ossDriver.Config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, ossDriver.Config))
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

	config := &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := testBuilder.BuildListers()

	d := NewDriver(ctx, config, &listers.StorageListers)

	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":            "oss",
		"REGISTRY_STORAGE_OSS_REGION": "oss-us-east-1",
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

	config := &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
		Region: "us-west-1",
	}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
					Region: "hidden",
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := testBuilder.BuildListers()

	d := NewDriver(ctx, config, &listers.StorageListers)

	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":            "oss",
		"REGISTRY_STORAGE_OSS_REGION": "oss-us-west-1",
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

	e := findEnvVar(envvars, "REGISTRY_STORAGE_OSS_ENDPOINT")
	if e != nil {
		t.Logf("env %s found,  %v", "REGISTRY_STORAGE_OSS_ENDPOINT", e)
	}
}

type tripper struct {
	req           int
	reqBodies     [][]byte
	responseCodes []int
	respBody      string
}

func (r *tripper) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		r.req++
	}()

	if req.Body != nil {
		dt, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.reqBodies = append(r.reqBodies, dt)
	}

	code := http.StatusOK
	if r.req < len(r.responseCodes) {
		code = r.responseCodes[r.req]
	}

	respBody := "{}"
	if r.respBody != "" {
		respBody = r.respBody
	}
	return &http.Response{
		StatusCode: code,
		Body:       ioutil.NopCloser(bytes.NewBufferString(respBody)),
	}, nil
}

func (r *tripper) AddResponse(code int) {
	r.responseCodes = append(r.responseCodes, code)
}

func (r *tripper) AddResponseBody(body string) {
	r.respBody = body
}

func TestStorageManagementState(t *testing.T) {
	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := builder.BuildListers()

	for _, tt := range []struct {
		name                    string
		config                  *imageregistryv1.Config
		responseCodes           []int
		respBody                string
		expectedManagementState string
	}{
		{
			name:                    "empty config",
			expectedManagementState: imageregistryv1.StorageManagementStateManaged,
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
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
						OSS:             &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
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
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
							Bucket: TestBucketName,
						},
					},
				},
			},
			responseCodes: []int{http.StatusOK},
			respBody: `
			<?xml version="1.0" encoding="UTF-8"?>
				<BucketInfo>
				  <Bucket>
					<CreationDate>2013-07-31T10:56:21.000Z</CreationDate>
					<StorageClass>Standard</StorageClass>
					<TransferAcceleration>Disabled</TransferAcceleration>
					<CrossRegionReplication>Disabled</CrossRegionReplication>
					<HierarchicalNamespace>Enabled</HierarchicalNamespace>
					<Name>` + TestBucketName + `</Name>
					<AccessControlList>
					  <Grant>private</Grant>
					</AccessControlList>
					<Comment>test</Comment>
				  </Bucket>
				</BucketInfo>
				`,
		},
		{
			name:                    "existing bucket provided (management set)",
			expectedManagementState: "foo",
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
							Bucket: TestAnotherBucketName,
						},
					},
				},
			},
			responseCodes: []int{http.StatusOK},
			respBody: `
			<?xml version="1.0" encoding="UTF-8"?>
				<BucketInfo>
				  <Bucket>
					<CreationDate>2013-07-31T10:56:21.000Z</CreationDate>
					<StorageClass>Standard</StorageClass>
					<TransferAcceleration>Disabled</TransferAcceleration>
					<CrossRegionReplication>Disabled</CrossRegionReplication>
					<HierarchicalNamespace>Enabled</HierarchicalNamespace>
					<Name>` + TestAnotherBucketName + `</Name>
					<AccessControlList>
					  <Grant>private</Grant>
					</AccessControlList>
					<Comment>test</Comment>
				  </Bucket>
				</BucketInfo>
				`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			drv := NewDriver(context.Background(), tt.config.Spec.Storage.OSS, &listers.StorageListers)
			rt := &tripper{}
			if len(tt.responseCodes) > 0 {
				for _, code := range tt.responseCodes {
					rt.AddResponse(code)
				}
			}
			if len(tt.respBody) > 0 {
				rt.AddResponseBody(tt.respBody)
			}

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

func TestUserProvidedTags(t *testing.T) {
	for _, tt := range []struct {
		name          string
		config        *imageregistryv1.Config
		userTags      []configv1.AlibabaCloudResourceTag
		expectedTags  []*oss.Tag
		responseCodes []int
		respBody      string
		infraName     string
		noTagRequest  bool
	}{
		{
			name:      "no user tags",
			infraName: "test-infra",
			expectedTags: []*oss.Tag{
				{
					Key:   "kubernetes.io/cluster/test-infra",
					Value: "owned",
				},
				{
					Key:   "Name",
					Value: "test-infra-image-registry",
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
					},
				},
			},
		},
		{
			name:      "with user tags but no bucket",
			infraName: "another-test-infra",
			userTags: []configv1.AlibabaCloudResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			expectedTags: []*oss.Tag{
				{
					Key:   "kubernetes.io/cluster/another-test-infra",
					Value: "owned",
				},
				{
					Key:   "Name",
					Value: "another-test-infra-image-registry",
				},
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{},
					},
				},
			},
		},
		{
			name:      "with user tags and unmanaged storage",
			infraName: "tinfra",
			userTags: []configv1.AlibabaCloudResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			noTagRequest: true,
			expectedTags: []*oss.Tag{},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "Unmanaged",
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
							Bucket: TestBucketName,
						},
					},
				},
			},
			respBody: `
			<?xml version="1.0" encoding="UTF-8"?>
				<BucketInfo>
				  <Bucket>
					<CreationDate>2013-07-31T10:56:21.000Z</CreationDate>
					<StorageClass>Standard</StorageClass>
					<TransferAcceleration>Disabled</TransferAcceleration>
					<CrossRegionReplication>Disabled</CrossRegionReplication>
					<HierarchicalNamespace>Enabled</HierarchicalNamespace>
					<Name>` + TestBucketName + `</Name>
					<AccessControlList>
					  <Grant>private</Grant>
					</AccessControlList>
					<Comment>test</Comment>
				  </Bucket>
				</BucketInfo>
				`,
		},
		{
			name:      "with user tags and already existing bucket",
			infraName: "tinfra",
			userTags: []configv1.AlibabaCloudResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			expectedTags: []*oss.Tag{
				{
					Key:   "kubernetes.io/cluster/tinfra",
					Value: "owned",
				},
				{
					Key:   "Name",
					Value: "tinfra-image-registry",
				},
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "Managed",
						OSS: &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
							Bucket: TestBucketName,
						},
					},
				},
			},
			respBody: `
			<?xml version="1.0" encoding="UTF-8"?>
				<BucketInfo>
				  <Bucket>
					<CreationDate>2013-07-31T10:56:21.000Z</CreationDate>
					<StorageClass>Standard</StorageClass>
					<TransferAcceleration>Disabled</TransferAcceleration>
					<CrossRegionReplication>Disabled</CrossRegionReplication>
					<HierarchicalNamespace>Enabled</HierarchicalNamespace>
					<Name>` + TestBucketName + `</Name>
					<AccessControlList>
					  <Grant>private</Grant>
					</AccessControlList>
					<Comment>test</Comment>
				  </Bucket>
				</BucketInfo>
				`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			builder := cirofake.NewFixturesBuilder()
			builder.AddInfraConfig(&configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: configv1.InfrastructureStatus{
					InfrastructureName: tt.infraName,
					PlatformStatus: &configv1.PlatformStatus{
						Type: configv1.AlibabaCloudPlatformType,
						AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
							ResourceTags: tt.userTags,
							Region:       "us-west-1",
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
					imageRegistrySecretDataKey: generateInitCredentialForSec(),
				},
			})
			listers := builder.BuildListers()

			drv := NewDriver(context.Background(), tt.config.Spec.Storage.OSS, &listers.StorageListers)
			rt := &tripper{}
			if len(tt.responseCodes) > 0 {
				for _, code := range tt.responseCodes {
					rt.AddResponse(code)
				}
			}

			if len(tt.respBody) > 0 {
				rt.AddResponseBody(tt.respBody)
			}
			drv.roundTripper = rt
			if err := drv.CreateStorage(tt.config); err != nil {
				t.Errorf("unexpected err %q", err)
				return
			}

		})
	}
}

func generateInitCredentialForSec() []byte {
	buf := &bytes.Buffer{}
	fmt.Fprint(buf, "[default]\n")
	fmt.Fprint(buf, "type = access_key\n")
	fmt.Fprintf(buf, "access_key_id = %s\n", TestAccessKeyId)
	fmt.Fprintf(buf, "access_key_secret = %s\n", TestAccessKeySecret)

	return buf.Bytes()
}

func Test_isInternal(t *testing.T) {
	ctx := context.Background()
	config := &imageregistryv1.ImageRegistryConfigStorageAlibabaOSS{
		EndpointAccessibility: imageregistryv1.InternalEndpoint,
	}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AlibabaCloudPlatformType,
				AlibabaCloud: &configv1.AlibabaCloudPlatformStatus{
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
			imageRegistrySecretDataKey: generateInitCredentialForSec(),
		},
	})
	listers := testBuilder.BuildListers()

	d := NewDriver(ctx, config, &listers.StorageListers)

	// internal
	result := d.isInternal()
	assert.Equal(t, true, result)

	//public
	d.Config.EndpointAccessibility = imageregistryv1.PublicEndpoint
	result = d.isInternal()
	assert.Equal(t, false, result)

	// empty
	d.Config.EndpointAccessibility = ""
	result = d.isInternal()
	assert.Equal(t, true, result)
}
