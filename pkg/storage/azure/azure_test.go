package azure

import (
	"context"
	"reflect"
	"regexp"
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
