package azure

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/mocks"
	"github.com/google/go-cmp/cmp"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapiv1 "github.com/openshift/api/operator/v1"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
)

const mockTenantID = "00000000-0000-0000-0000-000000000000"

type testDoer struct {
	response   *http.Response
	body       string
	statusCode int
	header     http.Header
}

// Do implements the Doer interface for mocking.
// Do accepts the passed policy request and body, then appends the response and emits it.
func (td *testDoer) Do(r *policy.Request) (resp *http.Response, err error) {
	// Helps in emitting sequential Responses for the same client
	if td.response != nil {
		return r.Next()
	}
	resp = &http.Response{
		StatusCode: td.statusCode,
		Request:    r.Raw(),
		Body:       io.NopCloser(bytes.NewBufferString(td.body)),
		Header:     td.header,
	}
	td.response = resp
	return resp, nil
}

func TestGetConfig(t *testing.T) {
	for _, tt := range []struct {
		name    string
		secrets []runtime.Object
		err     string
		result  *Azure
	}{
		{
			name: "no secrets",
			err: `unable to get cluster minted credentials: secret ` +
				`"installer-cloud-credentials" not found`,
		},
		{
			name: "no REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.ImageRegistryPrivateConfigurationUser,
						Namespace: "test",
					},
					Data: map[string][]byte{},
				},
			},
			err: `secret "test/image-registry-private-configuration-user" does not ` +
				`contain required key "REGISTRY_STORAGE_AZURE_ACCOUNTKEY"`,
		},
		{
			name: "empty REGISTRY_STORAGE_AZURE_ACCOUNTKEY",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.ImageRegistryPrivateConfigurationUser,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": []byte(""),
					},
				},
			},
			err: `the secret test/image-registry-private-configuration-user has an ` +
				`empty value for REGISTRY_STORAGE_AZURE_ACCOUNTKEY; the secret ` +
				`should be removed so that the operator can use cluster-wide ` +
				`secrets or it should contain a valid storage account access key`,
		},
		{
			name: "valid user provided secret",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.ImageRegistryPrivateConfigurationUser,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": []byte("abc"),
					},
				},
			},
			result: &Azure{
				AccountKey: "abc",
			},
		},
		{
			name: "user provided secret has priority",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.ImageRegistryPrivateConfigurationUser,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": []byte("cba"),
					},
				},
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.CloudCredentialsName,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"azure_subscription_id": []byte("subscription_id"),
						"azure_client_id":       []byte("client_id"),
						"azure_client_secret":   []byte("client_secret"),
						"azure_tenant_id":       []byte("tenant_id"),
						"azure_resourcegroup":   []byte("resourcegroup"),
						"azure_region":          []byte("region"),
					},
				},
			},
			result: &Azure{
				AccountKey: "cba",
			},
		},
		{
			name: "cloud credentials",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.CloudCredentialsName,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"azure_subscription_id": []byte("subscription_id"),
						"azure_client_id":       []byte("client_id"),
						"azure_client_secret":   []byte("client_secret"),
						"azure_tenant_id":       []byte("tenant_id"),
						"azure_resourcegroup":   []byte("resourcegroup"),
						"azure_region":          []byte("region"),
					},
				},
			},
			result: &Azure{
				SubscriptionID: "subscription_id",
				ClientID:       "client_id",
				ClientSecret:   "client_secret",
				TenantID:       "tenant_id",
				ResourceGroup:  "resourcegroup",
				Region:         "region",
			},
		},
		{
			name: "cloud credentials workload identity",
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      defaults.CloudCredentialsName,
						Namespace: "test",
					},
					Data: map[string][]byte{
						"azure_client_id":            []byte("client_id"),
						"azure_federated_token_file": []byte("/path/to/token"),
						"azure_region":               []byte("region"),
						"azure_subscription_id":      []byte("subscription_id"),
						"azure_tenant_id":            []byte("tenant_id"),
					},
				},
			},
			result: &Azure{
				SubscriptionID:     "subscription_id",
				ClientID:           "client_id",
				TenantID:           "tenant_id",
				ResourceGroup:      "resource-group-123",
				Region:             "region",
				FederatedTokenFile: "/path/to/token",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			for _, o := range tt.secrets {
				_ = indexer.Add(o)
			}
			lister := corev1listers.NewSecretLister(indexer)
			infraLister := fakeInfrastructureLister("resource-group-123")

			result, err := GetConfig(lister.Secrets("test"), infraLister)
			if len(tt.err) != 0 {
				if err == nil {
					t.Errorf("expected err %q, nil received instead", tt.err)
					return
				}
				if tt.err != err.Error() {
					t.Errorf("expected err %q, %q received instead", tt.err, err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected err %q", err)
				return
			}
			if !reflect.DeepEqual(tt.result, result) {
				t.Errorf("expected %v, received %v", tt.result, result)
			}
		})
	}
}

func fakeInfrastructureLister(resourceGroup string) configlisters.InfrastructureLister {
	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	err := fakeIndexer.Add(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: "user-j45xj",
			Platform:           configv1.OpenStackPlatformType,
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AzurePlatformType,
				Azure: &configv1.AzurePlatformStatus{
					ResourceGroupName: resourceGroup,
				},
			},
		},
	})
	if err != nil {
		panic(err) // should never happen
	}
	return configlisters.NewInfrastructureLister(fakeIndexer)
}

func TestGenerateAccountName(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-z]{3,24}$`)
	for _, infrastructureName := range []string{
		"",
		"foo",
		"foo-bar-baz",
		"FOO-BAR-3000",
		"1234567890123456789",
		"123456789012345678901234",
	} {
		accountName := generateAccountName(infrastructureName)
		t.Logf("infrastructureName=%q, accountName=%q", infrastructureName, accountName)
		if !re.MatchString(accountName) {
			t.Errorf("infrastructureName=%q: generated invalid account name: %q", infrastructureName, accountName)
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

func TestConfigEnvNonAzureStackHub(t *testing.T) {
	ctx := context.Background()

	cr := &imageregistryv1.Config{}
	config := &imageregistryv1.ImageRegistryConfigStorageAzure{}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AzurePlatformType,
				Azure: &configv1.AzurePlatformStatus{
					ResourceGroupName: "resourcegroup",
					CloudName:         configv1.AzureUSGovernmentCloud,
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
			"azure_subscription_id": []byte("subscription_id"),
			"azure_client_id":       []byte("client_id"),
			"azure_tenant_id":       []byte(mockTenantID),
			"azure_client_secret":   []byte("client_secret"),
			"azure_resourcegroup":   []byte("resourcegroup"),
		},
	})

	listers := testBuilder.BuildListers()

	authorizer := autorest.NullAuthorizer{}
	sender := mocks.NewSender()
	sender.AppendResponse(mocks.NewResponseWithContent(`{"nameAvailable":true}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`?`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"name":"account"}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`))

	d := NewDriver(ctx, config, &listers.StorageListers)
	d.authorizer = authorizer
	d.sender = sender
	d.policies = []policy.Policy{
		&testDoer{statusCode: http.StatusCreated},
	}
	err := d.CreateStorage(cr)
	if err != nil {
		t.Fatal(err)
	}
	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                  "azure",
		"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": "firstKey",
		"REGISTRY_STORAGE_AZURE_REALM":      "core.usgovcloudapi.net",
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

func TestConfigEnvWorkloadIdentityNonAzureStackHub(t *testing.T) {
	ctx := context.Background()

	config := &imageregistryv1.ImageRegistryConfigStorageAzure{}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AzurePlatformType,
				Azure: &configv1.AzurePlatformStatus{
					ResourceGroupName: "resourcegroup",
					CloudName:         configv1.AzureUSGovernmentCloud,
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
			"azure_client_id":            []byte("client_id"),
			"azure_federated_token_file": []byte("/path/to/file"),
			"azure_region":               []byte("region"),
			"azure_subscription_id":      []byte("subscription_id"),
			"azure_tenant_id":            []byte("tenant_id"),
		},
	})

	listers := testBuilder.BuildListers()

	authorizer := autorest.NullAuthorizer{}
	sender := mocks.NewSender()
	sender.AppendResponse(mocks.NewResponseWithContent(`{"nameAvailable":true}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`?`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"name":"account"}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`))
	sender.AppendResponse(mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`))

	d := NewDriver(ctx, config, &listers.StorageListers)
	d.authorizer = authorizer
	d.sender = sender
	d.policies = []policy.Policy{
		&testDoer{
			statusCode: http.StatusAccepted,
		},
	}

	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":           "azure",
		"AZURE_CLIENT_ID":            "client_id",
		"AZURE_TENANT_ID":            "tenant_id",
		"AZURE_FEDERATED_TOKEN_FILE": "/path/to/file",
		"AZURE_AUTHORITY_HOST":       "https://login.microsoftonline.com/", // default for configv1.AzureUSGovernmentCloud
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

func TestConfigEnvWithUserKey(t *testing.T) {
	ctx := context.Background()

	config := &imageregistryv1.ImageRegistryConfigStorageAzure{
		AccountName: "account",
		Container:   "container",
		CloudName:   "AzureUSGovernmentCloud",
	}

	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddSecrets(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.ImageRegistryPrivateConfigurationUser,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Data: map[string][]byte{
			"REGISTRY_STORAGE_AZURE_ACCOUNTKEY": []byte("key"),
		},
	})

	listers := testBuilder.BuildListers()

	d := NewDriver(ctx, config, &listers.StorageListers)
	envvars, err := d.ConfigEnv()
	if err != nil {
		t.Fatal(err)
	}

	expectedVars := map[string]interface{}{
		"REGISTRY_STORAGE":                   "azure",
		"REGISTRY_STORAGE_AZURE_CONTAINER":   "container",
		"REGISTRY_STORAGE_AZURE_ACCOUNTNAME": "account",
		"REGISTRY_STORAGE_AZURE_ACCOUNTKEY":  "key",
		"REGISTRY_STORAGE_AZURE_REALM":       "core.usgovcloudapi.net",
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

// custom sender for mocking
type sender struct {
	response []*http.Response
	body     string
}

// Do implements the Sender interface for mocking
// Do accepts the passed request and body, then appends the response and emits it.
func (s *sender) Do(r *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Request:    r,
		Body:       io.NopCloser(bytes.NewBufferString(s.body)),
	}
	s.response = append(s.response, resp)
	return resp, nil
}

func TestUserProvidedTags(t *testing.T) {
	for _, tt := range []struct {
		name         string
		userTags     []configv1.AzureResourceTag
		expectedTags map[string]string
		infraName    string
		responseBody string
	}{
		{
			name:      "no-user-tags",
			infraName: "some-infra",
			// only default tags
			expectedTags: map[string]string{
				"kubernetes.io_cluster.some-infra": "owned",
			},
			responseBody: `{"nameAvailable":true}`,
		},
		{
			name:      "with-user-tags",
			infraName: "test-infra",
			userTags: []configv1.AzureResourceTag{
				{
					Key:   "tag1",
					Value: "value1",
				},
				{
					Key:   "tag2",
					Value: "value2",
				},
			},
			// default tags and user tags
			expectedTags: map[string]string{
				"kubernetes.io_cluster.test-infra": "owned",
				"tag1":                             "value1",
				"tag2":                             "value2",
			},
			responseBody: `{"nameAvailable":true}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := &sender{
				body: tt.responseBody,
			}

			storageConfig := &imageregistryv1.ImageRegistryConfigStorageAzure{}

			drv := NewDriver(context.Background(), storageConfig, nil)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender

			_, _, err := drv.assureStorageAccount(
				&Azure{
					SubscriptionID: "subscription-id",
					ResourceGroup:  "resource-group",
				},
				&configv1.Infrastructure{
					Status: configv1.InfrastructureStatus{
						InfrastructureName: tt.infraName,
						Platform:           configv1.AzurePlatformType,
						PlatformStatus: &configv1.PlatformStatus{
							Type: configv1.AzurePlatformType,
							Azure: &configv1.AzurePlatformStatus{
								ResourceTags: tt.userTags,
							},
						},
					},
				},
			)
			if err != nil {
				t.Errorf("unexpected error %q", err)
			}

			// flag to confirm presence of tags
			foundTags := false

			for _, resp := range sender.response {
				if resp != nil && resp.Request != nil && resp.Request.Body != nil {

					reqBody := make(map[string]interface{})
					if err := json.NewDecoder(resp.Request.Body).Decode(&reqBody); err != nil {
						t.Fatalf("error decoding request: %q", err)
					}

					// ignore request without tags
					if _, ok := reqBody["tags"]; ok {
						foundTags = true

						tags, ok := reqBody["tags"].(map[string]interface{})
						if !ok {
							t.Fatal("unable to type assert tags field")
						}
						// convert into correct type
						receivedTags := make(map[string]string)
						for k, v := range tags {
							receivedTags[k] = fmt.Sprintf("%+v", v)
						}

						// compare the tags
						if !reflect.DeepEqual(tt.expectedTags, receivedTags) {
							t.Fatalf(
								"unexpected tags: %s",
								cmp.Diff(tt.expectedTags, receivedTags),
							)
						}
					}
				}
			}
			if !foundTags {
				t.Fatal("no tags present in the request")
			}
		})
	}
}

func Test_assureStorageAccount(t *testing.T) {
	for _, tt := range []struct {
		name          string
		storageConfig *imageregistryv1.ImageRegistryConfigStorageAzure
		mockResponses []*http.Response
		generated     bool
		err           string
		accountName   string
	}{
		{
			name:      "generate account name with success",
			generated: true,
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":true}`),
			},
		},
		{
			name: "fail to generate account name",
			err:  "create storage account failed, name not available",
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":false}`),
			},
		},
		{
			name: "error checking if account exists",
			err:  "storage.AccountsClient#CheckNameAvailability: Failure",
			mockResponses: []*http.Response{
				mocks.NewResponseWithStatus("NotFound", http.StatusNotFound),
			},
		},
		{
			name: "error creating account remotely",
			err:  "failed to start creating storage account",
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":true}`),
				mocks.NewResponseWithStatus("not found", http.StatusNotFound),
			},
		},
		{
			name:        "create account with provided account name",
			accountName: "myaccountname",
			generated:   true,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "myaccountname",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":true}`),
			},
		},
		{
			name:        "provided account name already exists",
			accountName: "myotheraccountname",
			generated:   false,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "myotheraccountname",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":false}`),
			},
		},
		{
			name: "invalid environment",
			err:  `There is no cloud environment matching the name "INVALID"`,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				CloudName: "invalid",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := mocks.NewSender()
			for _, response := range tt.mockResponses {
				sender.AppendResponse(response)
			}

			storageConfig := &imageregistryv1.ImageRegistryConfigStorageAzure{}
			if tt.storageConfig != nil {
				storageConfig = tt.storageConfig
			}

			drv := NewDriver(context.Background(), storageConfig, nil)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender

			name, generated, err := drv.assureStorageAccount(
				&Azure{
					SubscriptionID: "subscription_id",
					ResourceGroup:  "resource_group",
				},
				&configv1.Infrastructure{},
			)

			if err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			} else if len(tt.err) > 0 {
				t.Errorf("expected error %q, nil received instead", tt.err)
			}

			if generated != tt.generated {
				t.Errorf(
					"expected account generated to be %v, received %v instead",
					tt.generated,
					generated,
				)
			}

			if len(tt.accountName) != 0 && name != tt.accountName {
				t.Errorf(
					"expected account name %q, received %q instead",
					tt.accountName,
					name,
				)
			}
		})
	}
}

func Test_processUPI(t *testing.T) {
	for _, tt := range []struct {
		name            string
		registryConfig  *imageregistryv1.Config
		managementState string
		status          operatorapiv1.ConditionStatus
	}{
		{
			name:   "empty account and container name",
			status: operatorapiv1.ConditionFalse,
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{},
					},
				},
			},
		},
		{
			name:   "empty account name",
			status: operatorapiv1.ConditionFalse,
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							Container: "this_is_a_container_name",
						},
					},
				},
			},
		},
		{
			name:   "empty container name",
			status: operatorapiv1.ConditionFalse,
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "this_is_an_account_name",
						},
					},
				},
			},
		},
		{
			name:            "success",
			status:          operatorapiv1.ConditionTrue,
			managementState: imageregistryv1.StorageManagementStateUnmanaged,
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "this_is_an_account_name",
							Container:   "this_is_a_container_name",
						},
					},
				},
			},
		},
		{
			name:            "success with storage management state already set",
			status:          operatorapiv1.ConditionTrue,
			managementState: "foo",
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: "foo",
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "this_is_an_account_name",
							Container:   "this_is_a_container_name",
						},
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			NewDriver(
				context.Background(), tt.registryConfig.Spec.Storage.Azure, nil,
			).processUPI(tt.registryConfig)

			if tt.registryConfig.Spec.Storage.ManagementState != tt.managementState {
				t.Errorf(
					"expected storage management to be %q, %q instead",
					tt.managementState,
					tt.registryConfig.Spec.Storage.ManagementState,
				)
			}

			for _, cond := range tt.registryConfig.Status.Conditions {
				if cond.Type == defaults.StorageExists {
					if cond.Status != tt.status {
						t.Errorf(
							"expected status %q, %q instead",
							tt.status,
							cond.Status,
						)
					}
					return
				}
			}

			t.Errorf("%q condition type not found", defaults.StorageExists)
		})
	}
}

func Test_assureContainer(t *testing.T) {
	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AzurePlatformType,
				Azure: &configv1.AzurePlatformStatus{
					ResourceGroupName: "resourcegroup",
					CloudName:         configv1.AzureUSGovernmentCloud,
				},
			},
		},
	})
	listers := builder.BuildListers()

	for _, tt := range []struct {
		name          string
		storageConfig *imageregistryv1.ImageRegistryConfigStorageAzure
		mockResponses []*http.Response
		httpSender    func(int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error)
		generated     bool
		err           string
		containerName string
	}{
		{
			name:      "fails to create a new container (generating random container name)",
			err:       "azblob.newStorageError",
			generated: false,
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			httpSender: func(req int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
					return pipeline.NewHTTPResponse(
						mocks.NewResponseWithStatus("NotFound", http.StatusNotFound),
					), nil
				}
			},
		},
		{
			name:      "fail to check if container (provided by user) exists",
			err:       "unable to get the storage container",
			generated: false,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "account_name",
				Container:   "user-container",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			httpSender: func(req int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
					return pipeline.NewHTTPResponse(
						mocks.NewResponseWithStatus("NotFound", http.StatusNotFound),
					), nil
				}
			},
		},
		{
			name:          "use container provided by user (container exists)",
			containerName: "user-container",
			generated:     false,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "account_name",
				Container:   "user-container",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
		},
		{
			name:          "use container provided by user (container does not exist)",
			containerName: "user-container",
			generated:     true,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "account_name",
				Container:   "user-container",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			httpSender: func(req int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				if req == 0 {
					return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
						r := mocks.NewResponseWithStatus("", http.StatusNotFound)
						r.Header = map[string][]string{}
						r.Header.Add("x-ms-error-code", "ContainerNotFound")
						return pipeline.NewHTTPResponse(r), nil
					}
				}
				return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
					return pipeline.NewHTTPResponse(mocks.NewResponseWithContent(`{}`)), nil
				}
			},
		},
		{
			name: "fail to create container provided by user",
			err:  "azblob.newStorageError",
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "account_name",
				Container:   "user-container",
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			httpSender: func(req int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				if req == 0 {
					return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
						r := mocks.NewResponseWithStatus("", http.StatusNotFound)
						r.Header = map[string][]string{}
						r.Header.Add("x-ms-error-code", "ContainerNotFound")
						return pipeline.NewHTTPResponse(r), nil
					}
				}
				return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
					return pipeline.NewHTTPResponse(
						mocks.NewResponseWithStatus("NotFound", http.StatusNotFound),
					), nil
				}
			},
		},
		{
			name:      "generate container with success",
			generated: true,
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
		},
		{
			name: "invalid environment",
			err:  `There is no cloud environment matching the name "INVALID"`,
			storageConfig: &imageregistryv1.ImageRegistryConfigStorageAzure{
				CloudName: "invalid",
			},
		},
		{
			name: "fail to list keys",
			err:  "failed to get keys for the storage account",
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`---`),
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := mocks.NewSender()
			for _, response := range tt.mockResponses {
				sender.AppendResponse(response)
			}

			storageConfig := &imageregistryv1.ImageRegistryConfigStorageAzure{
				AccountName: "account_name",
			}
			if tt.storageConfig != nil {
				storageConfig = tt.storageConfig
			}

			drv := NewDriver(context.Background(), storageConfig, &listers.StorageListers)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender
			primaryKey = cachedKey{}

			var requestCounter int
			drv.httpSender = pipeline.FactoryFunc(
				func(_ pipeline.Policy, _ *pipeline.PolicyOptions) pipeline.PolicyFunc {
					defer func() {
						requestCounter++
					}()

					if tt.httpSender != nil {
						return tt.httpSender(requestCounter)
					}

					return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
						return pipeline.NewHTTPResponse(mocks.NewResponseWithContent(`{}`)), nil
					}
				},
			)

			name, generated, err := drv.assureContainer(
				&Azure{
					SubscriptionID: "subscription_id",
					ResourceGroup:  "resource_group",
				},
			)

			if err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			} else if len(tt.err) > 0 {
				t.Errorf("expected error %q, nil received instead", tt.err)
			}

			if generated != tt.generated {
				t.Errorf(
					"expected container generated to be %v, received %v instead",
					tt.generated,
					generated,
				)
			}

			if len(tt.containerName) != 0 && name != tt.containerName {
				t.Errorf(
					"expected container name %q, received %q instead",
					tt.containerName,
					name,
				)
			}
		})
	}
}

func Test_containerExists(t *testing.T) {
	for _, tt := range []struct {
		name          string
		httpSenderFn  func(context.Context, pipeline.Request) (pipeline.Response, error)
		accountName   string
		accountKey    string
		containerName string
		err           string
		exists        bool
	}{
		{
			name: "no account name neither account key",
		},
		{
			name:          "non existent container",
			accountName:   "account_name",
			accountKey:    base64.StdEncoding.EncodeToString([]byte("account_key")),
			containerName: "container_name",
			httpSenderFn: func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				resp := mocks.NewResponseWithStatus("NotFound", http.StatusNotFound)
				resp.Header = map[string][]string{}
				resp.Header.Add("x-ms-error-code", "ContainerNotFound")
				return pipeline.NewHTTPResponse(resp), nil
			},
		},
		{
			name:          "existent container",
			accountName:   "account_name",
			accountKey:    base64.StdEncoding.EncodeToString([]byte("account_key")),
			containerName: "container_name",
			exists:        true,
		},
		{
			name:          "unknown request error",
			accountName:   "account_name",
			accountKey:    base64.StdEncoding.EncodeToString([]byte("account_key")),
			containerName: "container_name",
			err:           "unable to get the storage container",
			httpSenderFn: func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
				return pipeline.NewHTTPResponse(
					mocks.NewResponseWithStatus("NotFound", http.StatusNotFound),
				), nil
			},
		},
		{
			name:          "invalid account name",
			accountName:   "account  name",
			accountKey:    base64.StdEncoding.EncodeToString([]byte("account_key")),
			containerName: "container_name",
			err:           `invalid character " " in host name`,
		},
		{
			name:          "invalid account key",
			accountName:   "account_name",
			accountKey:    "invalid base 64 string",
			containerName: "container_name",
			err:           "illegal base64 data",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			environment, err := getEnvironmentByName("")
			if err != nil {
				t.Fatalf("unexpected error when getting environment: %v", err)
			}

			drv := NewDriver(context.Background(), nil, nil)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = mocks.NewSender()
			drv.httpSender = pipeline.FactoryFunc(
				func(_ pipeline.Policy, _ *pipeline.PolicyOptions) pipeline.PolicyFunc {
					if tt.httpSenderFn != nil {
						return tt.httpSenderFn
					}
					return func(_ context.Context, _ pipeline.Request) (pipeline.Response, error) {
						return pipeline.NewHTTPResponse(mocks.NewResponseWithContent(`{}`)), nil
					}
				},
			)

			exists, err := drv.containerExists(
				context.Background(),
				environment,
				tt.accountName,
				tt.accountKey,
				tt.containerName,
			)

			if err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			} else if len(tt.err) > 0 {
				t.Errorf("expected error %q, nil received instead", tt.err)
			}

			if exists != tt.exists {
				t.Errorf("expected result to be %v, received %v", tt.exists, exists)
			}
		})
	}
}

func Test_storageManagementStateNonAzureStackHub(t *testing.T) {
	builder := cirofake.NewFixturesBuilder()
	builder.AddInfraConfig(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AzurePlatformType,
				Azure: &configv1.AzurePlatformStatus{
					ResourceGroupName: "resourcegroup",
					CloudName:         configv1.AzureUSGovernmentCloud,
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
			"azure_subscription_id": []byte("subscription_id"),
			"azure_client_id":       []byte("client_id"),
			"azure_tenant_id":       []byte(mockTenantID),
			"azure_client_secret":   []byte("client_secret"),
			"azure_resourcegroup":   []byte("resourcegroup"),
		},
	})
	listers := builder.BuildListers()
	containerNotFoundHeader := http.Header{}
	containerNotFoundHeader.Add("x-ms-error-code", "ContainerNotFound")

	for _, tt := range []struct {
		name           string
		registryConfig *imageregistryv1.Config
		mockResponses  []*http.Response
		policies       []policy.Policy
		err            string
		checkFn        func(*imageregistryv1.Config)
	}{
		{
			name:           "no config provided",
			registryConfig: &imageregistryv1.Config{},
			checkFn: func(cr *imageregistryv1.Config) {
				if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
					t.Errorf("expected to be managed, %q instead", cr.Spec.Storage.ManagementState)
				}
				if cr.Spec.Storage.Azure.AccountName == "" {
					t.Error("unexpected empty account name")
				}
				if cr.Spec.Storage.Azure.Container == "" {
					t.Error("unexpected empty container")
				}
			},
			policies: []policy.Policy{
				&testDoer{
					statusCode: http.StatusCreated,
				},
			},
		},
		{
			name: "user providing container and account name (both already exist)",
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":false}`),
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "foo_account",
							Container:   "foo_container",
						},
					},
				},
			},
			checkFn: func(cr *imageregistryv1.Config) {
				if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateUnmanaged {
					t.Errorf("expected to be unmanaged, %q instead", cr.Spec.Storage.ManagementState)
				}
				if cr.Spec.Storage.Azure.AccountName != "foo_account" {
					t.Errorf("account name has changed to %s", cr.Spec.Storage.Azure.AccountName)
				}
				if cr.Spec.Storage.Azure.Container != "foo_container" {
					t.Errorf("container has changed to %s", cr.Spec.Storage.Azure.Container)
				}
			},
			policies: []policy.Policy{
				&testDoer{
					statusCode: http.StatusOK,
				},
			},
		},
		{
			name: "user providing container and account name (both don't exist)",
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "foo_account",
							Container:   "foo_container",
						},
					},
				},
			},
			checkFn: func(cr *imageregistryv1.Config) {
				if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
					t.Errorf("expected to be managed, %q instead", cr.Spec.Storage.ManagementState)
				}
				if cr.Spec.Storage.Azure.AccountName != "foo_account" {
					t.Errorf("account name has changed to %s", cr.Spec.Storage.Azure.AccountName)
				}
				if cr.Spec.Storage.Azure.Container != "foo_container" {
					t.Errorf("container has changed to %s", cr.Spec.Storage.Azure.Container)
				}
			},
			policies: []policy.Policy{
				&testDoer{
					statusCode: http.StatusNotFound,
					header:     containerNotFoundHeader,
				},
				&testDoer{
					statusCode: http.StatusCreated,
				},
			},
		},
		{
			name: "user providing container and account name (only account name exists)",
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						Azure: &imageregistryv1.ImageRegistryConfigStorageAzure{
							AccountName: "foobar123",
							Container:   "foobar321",
						},
					},
				},
			},
			checkFn: func(cr *imageregistryv1.Config) {
				if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateManaged {
					t.Errorf("expected to be managed, %q instead", cr.Spec.Storage.ManagementState)
				}
				if cr.Spec.Storage.Azure.AccountName != "foobar123" {
					t.Errorf("account name has changed to %s", cr.Spec.Storage.Azure.AccountName)
				}
				if cr.Spec.Storage.Azure.Container != "foobar321" {
					t.Errorf("container has changed to %s", cr.Spec.Storage.Azure.Container)
				}
			},
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":false}`),
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
			},
			policies: []policy.Policy{
				&testDoer{
					statusCode: http.StatusNotFound,
					header:     containerNotFoundHeader,
				},
				&testDoer{
					statusCode: http.StatusCreated,
				},
			},
		},
		{
			name: "do not overwrite management state already set by user",
			registryConfig: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					Storage: imageregistryv1.ImageRegistryConfigStorage{
						ManagementState: imageregistryv1.StorageManagementStateUnmanaged,
					},
				},
			},
			checkFn: func(cr *imageregistryv1.Config) {
				if cr.Spec.Storage.ManagementState != imageregistryv1.StorageManagementStateUnmanaged {
					t.Errorf("expected to be unmanaged, %q instead", cr.Spec.Storage.ManagementState)
				}
				if cr.Spec.Storage.Azure.AccountName == "" {
					t.Error("unexpected empty account name")
				}
				if cr.Spec.Storage.Azure.Container == "" {
					t.Error("unexpected empty container")
				}
			},
			policies: []policy.Policy{
				&testDoer{statusCode: http.StatusCreated},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sender := mocks.NewSender()
			if len(tt.mockResponses) > 0 {
				for _, resp := range tt.mockResponses {
					sender.AppendResponse(resp)
				}
			} else {
				sender.AppendResponse(mocks.NewResponseWithContent(`{"nameAvailable":true}`))
				sender.AppendResponse(mocks.NewResponseWithContent(`?`))
				sender.AppendResponse(mocks.NewResponseWithContent(`{"name":"account"}`))
				sender.AppendResponse(mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`))
			}

			storageConfig := tt.registryConfig.Spec.Storage.Azure
			if tt.registryConfig.Spec.Storage.Azure == nil {
				storageConfig = &imageregistryv1.ImageRegistryConfigStorageAzure{}
			}

			drv := NewDriver(
				context.Background(),
				storageConfig,
				&listers.StorageListers,
			)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender
			if tt.policies != nil {
				drv.policies = tt.policies
			}

			if err := drv.CreateStorage(tt.registryConfig); err != nil {
				if len(tt.err) == 0 {
					t.Errorf("unexpected error: %v", err)
				} else if !strings.Contains(err.Error(), tt.err) {
					t.Errorf(
						"expected error to be %q, %v received instead",
						tt.err,
						err,
					)
				}
			} else if len(tt.err) > 0 {
				t.Errorf("expected error %q, nil received instead", tt.err)
			}

			tt.checkFn(tt.registryConfig)
		})
	}
}
