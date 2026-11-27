package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/endpoints"
	"github.com/aws/aws-sdk-go/private/protocol/xml/xmlutil"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/go-cmp/cmp"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

func TestEndpointsResolver(t *testing.T) {
	testCases := []struct {
		region       string
		useDualStack bool
		endpoint     string
	}{
		{
			region:   "us-east-1",
			endpoint: "https://s3.amazonaws.com",
		},
		{
			region:       "us-east-1",
			useDualStack: true,
			endpoint:     "https://s3.dualstack.us-east-1.amazonaws.com",
		},
		{
			region:   "us-gov-east-1",
			endpoint: "https://s3.us-gov-east-1.amazonaws.com",
		},
		{
			region:       "us-gov-east-1",
			useDualStack: true,
			endpoint:     "https://s3.dualstack.us-gov-east-1.amazonaws.com",
		},
		{
			region:   "us-iso-east-1",
			endpoint: "https://s3.us-iso-east-1.c2s.ic.gov",
		},
		{
			region:       "us-iso-east-1",
			useDualStack: true,
			endpoint:     "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			er := newEndpointsResolver(tc.region, "", nil)
			var opts []func(*endpoints.Options)
			if tc.useDualStack {
				opts = append(opts, endpoints.UseDualStackEndpointOption)
			}
			ep, err := er.EndpointFor("s3", tc.region, opts...)
			if isUnknownEndpointError(err) && tc.endpoint == "" {
				return // the error is expected
			}
			if err != nil {
				t.Fatal(err)
			}
			if ep.URL != tc.endpoint {
				t.Errorf("got %s, want %s", ep.URL, tc.endpoint)
			}
		})
	}
}

func TestRegionS3DualStack(t *testing.T) {
	testCases := []struct {
		region string
		want   bool
	}{
		{
			region: "us-east-1",
			want:   true,
		},
		{
			region: "us-gov-east-1",
			want:   true,
		},
		{
			region: "us-iso-east-1",
			want:   false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.region, func(t *testing.T) {
			got, err := regionHasDualStackS3(tc.region)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %t, want %t", got, tc.want)
			}
		})
	}
}

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
		Listers: &listers.StorageListers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageS3{},
	}

	err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageS3{
		Region: "us-east-1",
	}

	if !reflect.DeepEqual(s3Driver.Config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, s3Driver.Config))
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
		Listers: &listers.StorageListers,
		Config:  &imageregistryv1.ImageRegistryConfigStorageS3{},
	}
	err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Fatal(err)
	}

	expected := &imageregistryv1.ImageRegistryConfigStorageS3{
		Region:             "example",
		RegionEndpoint:     "https://s3.example.com",
		VirtualHostedStyle: true,
	}
	if !reflect.DeepEqual(s3Driver.Config, expected) {
		t.Errorf("unexpected config: %s", cmp.Diff(expected, s3Driver.Config))
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

	TestFeatureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
		[]configv1.FeatureGateName{util.TestFeatureGateName},
		[]configv1.FeatureGateName{},
	)

	d := NewDriver(ctx, config, &listers.StorageListers, TestFeatureGateAccessor)

	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                          "s3",
		"REGISTRY_STORAGE_S3_REGION":                "us-east-1",
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          true,
		"REGISTRY_STORAGE_S3_FORCEPATHSTYLE":        true,
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

	TestFeatureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
		[]configv1.FeatureGateName{util.TestFeatureGateName},
		[]configv1.FeatureGateName{},
	)

	d := NewDriver(ctx, config, &listers.StorageListers, TestFeatureGateAccessor)

	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                          "s3",
		"REGISTRY_STORAGE_S3_REGION":                "us-west-1",
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          true,
		"REGISTRY_STORAGE_S3_FORCEPATHSTYLE":        true,
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
	reqBodies     [][]byte
	responseCodes []int
}

func (r *tripper) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		r.req++
	}()

	if req.Body != nil {
		dt, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		r.reqBodies = append(r.reqBodies, dt)
	}

	code := http.StatusOK
	if r.req < len(r.responseCodes) {
		code = r.responseCodes[r.req]
	}

	return &http.Response{
		StatusCode: code,
		Body:       io.NopCloser(bytes.NewBufferString("{}")),
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
			TestFeatureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
				[]configv1.FeatureGateName{util.TestFeatureGateName},
				[]configv1.FeatureGateName{},
			)
			drv := NewDriver(context.Background(), tt.config.Spec.Storage.S3, &listers.StorageListers, TestFeatureGateAccessor)

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
		userTags      []configv1.AWSResourceTag
		expectedTags  []*s3.Tag
		responseCodes []int
		infraName     string
		noTagRequest  bool
	}{
		{
			name:      "no user tags",
			infraName: "test-infra",
			expectedTags: []*s3.Tag{
				{
					Key:   aws.String("kubernetes.io/cluster/test-infra"),
					Value: aws.String("owned"),
				},
				{
					Key:   aws.String("Name"),
					Value: aws.String("test-infra-image-registry"),
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
					},
				},
			},
		},
		{
			name:      "with user tags but no bucket",
			infraName: "another-test-infra",
			userTags: []configv1.AWSResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			expectedTags: []*s3.Tag{
				{
					Key:   aws.String("kubernetes.io/cluster/another-test-infra"),
					Value: aws.String("owned"),
				},
				{
					Key:   aws.String("Name"),
					Value: aws.String("another-test-infra-image-registry"),
				},
				{
					Key:   aws.String("tag0"),
					Value: aws.String("value0"),
				},
				{
					Key:   aws.String("tag1"),
					Value: aws.String("value1"),
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
					},
				},
			},
		},
		{
			name:      "with user tags and unmanaged storage",
			infraName: "tinfra",
			userTags: []configv1.AWSResourceTag{
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
			expectedTags: []*s3.Tag{},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "Unmanaged",
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "a-bucket",
						},
					},
				},
			},
		},
		{
			name:      "with user tags and already existing bucket",
			infraName: "tinfra",
			userTags: []configv1.AWSResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			expectedTags: []*s3.Tag{
				{
					Key:   aws.String("kubernetes.io/cluster/tinfra"),
					Value: aws.String("owned"),
				},
				{
					Key:   aws.String("Name"),
					Value: aws.String("tinfra-image-registry"),
				},
				{
					Key:   aws.String("tag0"),
					Value: aws.String("value0"),
				},
				{
					Key:   aws.String("tag1"),
					Value: aws.String("value1"),
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "Managed",
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "a-bucket",
						},
					},
				},
			},
		},
		{
			name:      "with user tags and creating provided bucket",
			infraName: "tinfra",
			userTags: []configv1.AWSResourceTag{
				{
					Key:   "tag0",
					Value: "value0",
				},
				{
					Key:   "tag1",
					Value: "value1",
				},
			},
			responseCodes: []int{http.StatusNotFound},
			expectedTags: []*s3.Tag{
				{
					Key:   aws.String("kubernetes.io/cluster/tinfra"),
					Value: aws.String("owned"),
				},
				{
					Key:   aws.String("Name"),
					Value: aws.String("tinfra-image-registry"),
				},
				{
					Key:   aws.String("tag0"),
					Value: aws.String("value0"),
				},
				{
					Key:   aws.String("tag1"),
					Value: aws.String("value1"),
				},
			},
			config: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "Managed",
						S3: &imageregistryv1.ImageRegistryConfigStorageS3{
							Bucket: "a-bucket",
						},
					},
				},
			},
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
						Type: configv1.AWSPlatformType,
						AWS: &configv1.AWSPlatformStatus{
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
					"aws_access_key_id":     []byte("access_key_id"),
					"aws_secret_access_key": []byte("secret_access_key"),
				},
			})
			listers := builder.BuildListers()
			TestFeatureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
				[]configv1.FeatureGateName{util.TestFeatureGateName},
				[]configv1.FeatureGateName{},
			)
			drv := NewDriver(context.Background(), tt.config.Spec.Storage.S3, &listers.StorageListers, TestFeatureGateAccessor)
			rt := &tripper{}
			if len(tt.responseCodes) > 0 {
				for _, code := range tt.responseCodes {
					rt.AddResponse(code)
				}
			}
			drv.roundTripper = rt

			if err := drv.CreateStorage(tt.config); err != nil {
				t.Errorf("unexpected err %q", err)
				return
			}

			for _, body := range rt.reqBodies {
				// ignore any other types of request.
				if !strings.Contains(string(body), "Tagging") {
					continue
				}

				buf := bytes.NewBuffer(body)
				tagging := s3.Tagging{}
				xmldec := xml.NewDecoder(buf)

				if err := xmlutil.UnmarshalXML(&tagging, xmldec, ""); err != nil {
					t.Fatalf("error decoding tagging request: %s", err)
				}

				if !reflect.DeepEqual(tagging.TagSet, tt.expectedTags) {
					t.Fatalf(
						"expected tags %+v, received %+v",
						tt.expectedTags, tagging.TagSet,
					)
				}
				return
			}

			if tt.noTagRequest {
				return
			}
			t.Fatal("no request for tagging bucket found")
		})
	}
}

// s3ErrorTripper is an http.RoundTripper that can return S3 XML error responses
// for specific request paths, allowing tests to simulate BucketAlreadyOwnedByYou,
// BucketAlreadyExists, and other S3 error codes.
type s3ErrorTripper struct {
	// responses maps request index to a response. If a request index is not
	// in the map, a 200 OK with empty body is returned.
	responses []s3ErrorResponse
	req       int
}

type s3ErrorResponse struct {
	code    int
	errCode string // S3 error code, e.g. "BucketAlreadyOwnedByYou"
}

func (r *s3ErrorTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() { r.req++ }()

	if r.req < len(r.responses) {
		resp := r.responses[r.req]
		if resp.errCode != "" {
			body := fmt.Sprintf(
				`<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code><Message>error</Message></Error>`,
				resp.errCode,
			)
			return &http.Response{
				StatusCode: resp.code,
				Body:       io.NopCloser(bytes.NewBufferString(body)),
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
			}, nil
		}
		return &http.Response{
			StatusCode: resp.code,
			Body:       io.NopCloser(bytes.NewBufferString("")),
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewBufferString("")),
	}, nil
}

func TestCreateStorageDeterministicNaming(t *testing.T) {
	infraName := "test-cluster-abc12"
	expectedBucket := infraName + "-" + defaults.ImageRegistryName + "-us-east-1"

	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: infraName,
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "us-east-1",
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
			"aws_access_key_id":     []byte("access"),
			"aws_secret_access_key": []byte("secret"),
		},
	})
	listers := builder.BuildListers()

	newDriver := func(bucket string) *driver {
		fg := featuregates.NewHardcodedFeatureGateAccess(
			[]configv1.FeatureGateName{util.TestFeatureGateName},
			[]configv1.FeatureGateName{},
		)
		return NewDriver(
			context.Background(),
			&imageregistryv1.ImageRegistryConfigStorageS3{
				Bucket: bucket,
				Region: "us-east-1",
			},
			&listers.StorageListers,
			fg,
		)
	}

	newConfig := func() *imageregistryv1.Config {
		return &imageregistryv1.Config{
			Spec: imageregistryv1.ImageRegistrySpec{
				Storage: imageregistryv1.ImageRegistryConfigStorage{
					S3: &imageregistryv1.ImageRegistryConfigStorageS3{
						Region: "us-east-1",
					},
				},
			},
		}
	}

	for _, tt := range []struct {
		name       string
		bucket     string
		responses  []s3ErrorResponse
		wantErr    bool
		wantErrMsg string
		wantBucket string
	}{
		{
			name:       "When bucket name is empty, it should generate a deterministic name and create the bucket",
			bucket:     "",
			responses:  nil, // all 200 OK
			wantBucket: expectedBucket,
		},
		{
			name:   "When bucket already owned by us, it should reuse it without error",
			bucket: "",
			responses: []s3ErrorResponse{
				// CreateBucket returns BucketAlreadyOwnedByYou
				{code: http.StatusConflict, errCode: "BucketAlreadyOwnedByYou"},
			},
			wantBucket: expectedBucket,
		},
		{
			name:   "When bucket owned by another account, it should fail with an error",
			bucket: "",
			responses: []s3ErrorResponse{
				// CreateBucket returns BucketAlreadyExists
				{code: http.StatusConflict, errCode: "BucketAlreadyExists"},
			},
			wantErr:    true,
			wantErrMsg: "owned by another account",
		},
		{
			name:       "When bucket name is pre-configured, it should use the configured name",
			bucket:     "my-custom-bucket",
			responses:  nil, // all 200 OK (HeadBucket succeeds)
			wantBucket: "my-custom-bucket",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			drv := newDriver(tt.bucket)
			drv.roundTripper = &s3ErrorTripper{responses: tt.responses}

			config := newConfig()
			if tt.bucket != "" {
				config.Spec.Storage.S3.Bucket = tt.bucket
			}

			err := drv.CreateStorage(config)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error but got nil")
				}
				if tt.wantErrMsg != "" && !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("expected error containing %q, got %q", tt.wantErrMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantBucket != "" && config.Spec.Storage.S3.Bucket != tt.wantBucket {
				t.Errorf("expected bucket %q in spec, got %q", tt.wantBucket, config.Spec.Storage.S3.Bucket)
			}

			if tt.wantBucket != "" {
				if config.Status.Storage.S3 == nil {
					t.Fatalf("expected status.storage.s3 to be set")
				}
				if config.Status.Storage.S3.Bucket != tt.wantBucket {
					t.Errorf("expected bucket %q in status, got %q", tt.wantBucket, config.Status.Storage.S3.Bucket)
				}
			}

			if config.Spec.Storage.ManagementState == "" {
				t.Error("expected ManagementState to be set, got empty")
			}
		})
	}
}
