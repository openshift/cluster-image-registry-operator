package azure

import (
	"context"
	"encoding/base64"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Azure/azure-pipeline-go/pipeline"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/mocks"

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
	} {
		t.Run(tt.name, func(t *testing.T) {
			indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
			for _, o := range tt.secrets {
				_ = indexer.Add(o)
			}
			lister := corev1listers.NewSecretLister(indexer)

			result, err := GetConfig(lister.Secrets("test"))
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

func TestGenerateAccountName(t *testing.T) {
	var re = regexp.MustCompile(`^[0-9a-z]{3,24}$`)
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

func TestConfigEnv(t *testing.T) {
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

	httpSender := pipeline.FactoryFunc(func(next pipeline.Policy, po *pipeline.PolicyOptions) pipeline.PolicyFunc {
		return func(ctx context.Context, request pipeline.Request) (pipeline.Response, error) {
			return pipeline.NewHTTPResponse(mocks.NewResponseWithContent(`{}`)), nil
		}
	})

	d := NewDriver(ctx, config, listers)
	d.authorizer = authorizer
	d.sender = sender
	d.httpSender = httpSender
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

	d := NewDriver(ctx, config, listers)
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

			drv := NewDriver(context.Background(), storageConfig, listers)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender

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

func Test_storageManagementState(t *testing.T) {
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
			"azure_client_secret":   []byte("client_secret"),
			"azure_resourcegroup":   []byte("resourcegroup"),
		},
	})
	listers := builder.BuildListers()

	for _, tt := range []struct {
		name           string
		registryConfig *imageregistryv1.Config
		mockResponses  []*http.Response
		httpSender     func(int) func(_ context.Context, _ pipeline.Request) (pipeline.Response, error)
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
			mockResponses: []*http.Response{
				mocks.NewResponseWithContent(`{"nameAvailable":false}`),
				mocks.NewResponseWithContent(`{"keys":[{"value":"firstKey"}]}`),
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
				listers,
			)
			drv.authorizer = autorest.NullAuthorizer{}
			drv.sender = sender

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
