package swift

import (
	"fmt"
	"net/http"
	"testing"

	th "github.com/gophercloud/gophercloud/testhelper"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

const (
	username  = "myUsername"
	password  = "myPassword"
	container = "registry"
	domain    = "Default"
	tenant    = "openshift-registry"

	cloudName      = "openstack"
	cloudSecretKey = "clouds.yaml"

	upiSecretName = "image-registry-private-configuration-user"
	ipiSecretName = "installer-cloud-credentials"
)

var (
	// Fake Swift credentials map
	fakeSecretData = map[string][]byte{
		"REGISTRY_STORAGE_SWIFT_USERNAME": []byte(username),
		"REGISTRY_STORAGE_SWIFT_PASSWORD": []byte(password),
	}
	fakeCloudsYAML map[string][]byte
)

type MockSecretNamespaceLister interface {
	Get(string) (*corev1.Secret, error)
	List(selector labels.Selector) ([]*corev1.Secret, error)
}
type MockUPISecretNamespaceLister struct{}

func (m MockUPISecretNamespaceLister) Get(name string) (*corev1.Secret, error) {
	if name == upiSecretName {
		return &corev1.Secret{
			Data: fakeSecretData,
		}, nil
	}

	return nil, &k8serrors.StatusError{metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotFound,
		Reason:  metav1.StatusReasonNotFound,
		Details: &metav1.StatusDetails{},
		Message: fmt.Sprintf("No secret with name %v was found", name),
	}}
}

func (m MockUPISecretNamespaceLister) List(selector labels.Selector) ([]*corev1.Secret, error) {
	return []*corev1.Secret{
		{
			Data: fakeSecretData,
		},
	}, nil
}

type MockIPISecretNamespaceLister struct{}

func (m MockIPISecretNamespaceLister) Get(name string) (*corev1.Secret, error) {
	if name == ipiSecretName {
		return &corev1.Secret{
			Data: fakeCloudsYAML,
		}, nil
	}

	return nil, &k8serrors.StatusError{metav1.Status{
		Status:  metav1.StatusFailure,
		Code:    http.StatusNotFound,
		Reason:  metav1.StatusReasonNotFound,
		Details: &metav1.StatusDetails{},
		Message: fmt.Sprintf("No secret with name %v was found", name),
	}}
}

func (m MockIPISecretNamespaceLister) List(selector labels.Selector) ([]*corev1.Secret, error) {
	return []*corev1.Secret{
		{
			Data: fakeCloudsYAML,
		},
	}, nil
}

func handleAuthentication(t *testing.T, endpointType string) {
	th.Mux.HandleFunc("/v3/auth/tokens", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")
		th.TestJSONRequest(t, r, `{
			"auth": {
			  "identity": {
				"methods": [
				  "password"
				],
				"password": {
				  "user": {
					"domain": {
					  "name": "`+domain+`"
					},
					"name": "`+username+`",
					"password": "`+password+`"
				  }
				}
			  },
			  "scope": {
				"project": {
				  "domain": {
					"name": "`+domain+`"
				  },
				  "name": "`+tenant+`"
				}
			  }
			}
		  }`)

		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{
			"token": {
				"expires_at": "2030-10-02T13:45:00.000000Z",
				"catalog": [{
					"endpoints": [{
					"url": "`+th.Endpoint()+`",
					"interface": "public",
					"id": "29beb2f1567642eb810b042b6719ea88",
					"region": "RegionOne",
					"region_id": "RegionOne"
					}],
					"type": "`+endpointType+`",
					"name": "swift"
				}]
			}
		}`)
	})
}

func fakeInfrastructureLister(cloudName string) configlisters.InfrastructureLister {
	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	fakeIndexer.Add(&configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: "user-j45xj",
			Platform:           configv1.OpenStackPlatformType,
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.OpenStackPlatformType,
				OpenStack: &configv1.OpenStackPlatformStatus{
					CloudName: cloudName,
				},
			},
		},
	})
	return configlisters.NewInfrastructureLister(fakeIndexer)
}

func mockConfig(includeStatus bool, endpoint string, secretLister MockSecretNamespaceLister) (driver, imageregistryv1.Config) {
	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		AuthURL:   endpoint,
		Container: container,
		Domain:    domain,
		Tenant:    tenant,
	}

	d := driver{
		Listers: &regopclient.Listers{
			Secrets:         secretLister,
			Infrastructures: fakeInfrastructureLister(cloudName),
		},
		Config: &config,
	}

	ic := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
	}

	if includeStatus {
		ic.Status = imageregistryv1.ImageRegistryStatus{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
			StorageManaged: true,
		}
	}

	return d, ic
}

func TestSwiftCreateStorageNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	numRequests := 0

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		// The first request should be a head request
		// to check if container with name exists
		if numRequests == 0 {
			th.TestMethod(t, r, "HEAD")
			th.TestHeader(t, r, "Accept", "application/json")
			w.WriteHeader(http.StatusNotFound)
			numRequests++
		} else {
			// Second request should be the actual create
			th.TestMethod(t, r, "PUT")
			th.TestHeader(t, r, "Accept", "application/json")

			w.Header().Set("Content-Length", "0")
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
			w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0058b4ba37")
			w.WriteHeader(http.StatusNoContent)
		}
	})

	d, installConfig := mockConfig(false, th.Endpoint()+"v3", MockUPISecretNamespaceLister{})

	d.CreateStorage(&installConfig)

	th.AssertEquals(t, true, installConfig.Status.StorageManaged)
	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionTrue, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, container, installConfig.Status.Storage.Swift.Container)
}

func TestSwiftRemoveStorageNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "DELETE")
		th.TestHeader(t, r, "Accept", "application/json")
		w.WriteHeader(http.StatusNoContent)
	})

	d, installConfig := mockConfig(true, th.Endpoint()+"v3", MockUPISecretNamespaceLister{})

	d.RemoveStorage(&installConfig)

	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionFalse, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, "", installConfig.Status.Storage.Swift.Container)
}

func TestSwiftStorageExistsNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "HEAD")
		th.TestHeader(t, r, "Accept", "application/json")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
		w.Header().Set("X-Container-Bytes-Used", "100")
		w.Header().Set("X-Container-Object-Count", "4")
		w.Header().Set("X-Container-Read", "test")
		w.Header().Set("X-Container-Write", "test2,user4")
		w.Header().Set("X-Timestamp", "1471298837.95721")
		w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0057b4ba37")
		w.Header().Set("X-Storage-Policy", "test_policy")
		w.WriteHeader(http.StatusNoContent)
	})

	d, installConfig := mockConfig(false, th.Endpoint()+"v3", MockUPISecretNamespaceLister{})

	res, err := d.StorageExists(&installConfig)

	th.AssertNoErr(t, err)
	th.AssertEquals(t, true, res)
}

func TestSwiftSecrets(t *testing.T) {
	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		AuthURL:   "http://localhost:5000/v3",
		Container: container,
		Domain:    domain,
		Tenant:    tenant,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets:         MockUPISecretNamespaceLister{},
			Infrastructures: fakeInfrastructureLister(cloudName),
		},
		Config: &config,
	}
	res, err := d.Secrets()
	th.AssertNoErr(t, err)
	th.AssertEquals(t, 2, len(res))
	th.AssertEquals(t, username, res["REGISTRY_STORAGE_SWIFT_USERNAME"])
	th.AssertEquals(t, password, res["REGISTRY_STORAGE_SWIFT_PASSWORD"])

	config = imageregistryv1.ImageRegistryConfigStorageSwift{
		Container: container,
	}
	// Support any cloud name provided by platform status
	customCloud := "myCloud"
	d = driver{
		Listers: &regopclient.Listers{
			Secrets:         MockIPISecretNamespaceLister{},
			Infrastructures: fakeInfrastructureLister(customCloud),
		},
		Config: &config,
	}
	fakeCloudsYAMLData := []byte(`clouds:
  ` + customCloud + `:
    auth:
      auth_url: "http://localhost:5000/v3"
      project_name: ` + tenant + `
      username: ` + username + `
      password: ` + password + `
      domain_name: ` + domain + `
    region_name: RegionOne`)

	fakeCloudsYAML = map[string][]byte{
		cloudSecretKey: fakeCloudsYAMLData,
	}
	res, err = d.Secrets()
	th.AssertNoErr(t, err)
	th.AssertEquals(t, 2, len(res))
	th.AssertEquals(t, username, res["REGISTRY_STORAGE_SWIFT_USERNAME"])
	th.AssertEquals(t, password, res["REGISTRY_STORAGE_SWIFT_PASSWORD"])
}

func TestSwiftCreateStorageCloudConfig(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	fakeCloudsYAMLData := []byte(`clouds:
  ` + cloudName + `:
    auth:
      auth_url: ` + th.Endpoint() + "v3" + `
      project_name: ` + tenant + `
      username: ` + username + `
      password: ` + password + `
      domain_name: ` + domain + `
    region_name: RegionOne`)

	fakeCloudsYAML = map[string][]byte{
		cloudSecretKey: fakeCloudsYAMLData,
	}

	numRequests := 0

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		if numRequests == 0 {
			th.TestMethod(t, r, "HEAD")
			th.TestHeader(t, r, "Accept", "application/json")
			w.WriteHeader(http.StatusNotFound)
			numRequests++
		} else {
			th.TestMethod(t, r, "PUT")
			th.TestHeader(t, r, "Accept", "application/json")

			w.Header().Set("Content-Length", "0")
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
			w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0058b4ba37")
			w.WriteHeader(http.StatusNoContent)
		}
	})

	d, installConfig := mockConfig(false, th.Endpoint()+"v3", MockIPISecretNamespaceLister{})

	d.CreateStorage(&installConfig)

	th.AssertEquals(t, true, installConfig.Status.StorageManaged)
	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionTrue, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, container, installConfig.Status.Storage.Swift.Container)
}

func TestSwiftRemoveStorageCloudConfig(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	fakeCloudsYAMLData := []byte(`clouds:
  ` + cloudName + `:
    auth:
      auth_url: ` + th.Endpoint() + "v3" + `
      project_name: ` + tenant + `
      username: ` + username + `
      password: ` + password + `
      domain_name: ` + domain + `
    region_name: RegionOne`)

	fakeCloudsYAML = map[string][]byte{
		cloudSecretKey: fakeCloudsYAMLData,
	}

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "DELETE")
		th.TestHeader(t, r, "Accept", "application/json")
		w.WriteHeader(http.StatusNoContent)
	})

	d, installConfig := mockConfig(true, th.Endpoint()+"v3", MockIPISecretNamespaceLister{})

	d.RemoveStorage(&installConfig)

	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionFalse, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, "", installConfig.Status.Storage.Swift.Container)
}

func TestSwiftStorageExistsCloudConfig(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "container")

	fakeCloudsYAMLData := []byte(`clouds:
  ` + cloudName + `:
    auth:
      auth_url: ` + th.Endpoint() + "v3" + `
      project_name: ` + tenant + `
      username: ` + username + `
      password: ` + password + `
      domain_name: ` + domain + `
    region_name: RegionOne`)

	fakeCloudsYAML = map[string][]byte{
		cloudSecretKey: fakeCloudsYAMLData,
	}

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "HEAD")
		th.TestHeader(t, r, "Accept", "application/json")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
		w.Header().Set("X-Container-Bytes-Used", "100")
		w.Header().Set("X-Container-Object-Count", "4")
		w.Header().Set("X-Container-Read", "test")
		w.Header().Set("X-Container-Write", "test2,user4")
		w.Header().Set("X-Timestamp", "1471298837.95721")
		w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0057b4ba37")
		w.Header().Set("X-Storage-Policy", "test_policy")
		w.WriteHeader(http.StatusNoContent)
	})

	d, installConfig := mockConfig(false, th.Endpoint()+"v3", MockIPISecretNamespaceLister{})

	res, err := d.StorageExists(&installConfig)

	th.AssertNoErr(t, err)
	th.AssertEquals(t, true, res)
}

func TestSwiftConfigEnvCloudConfig(t *testing.T) {
	fakeCloudsYAMLData := []byte(`clouds:
  ` + cloudName + `:
    auth:
      auth_url: http://localhost:5000/v3
      project_name: ` + tenant + `
      username: ` + username + `
      password: ` + password + `
      domain_name: ` + domain + `
    region_name: RegionOne`)

	fakeCloudsYAML = map[string][]byte{
		cloudSecretKey: fakeCloudsYAMLData,
	}

	d, _ := mockConfig(false, "http://localhost:5000/v3", MockIPISecretNamespaceLister{})

	res, err := d.ConfigEnv()

	th.AssertNoErr(t, err)
	th.AssertEquals(t, "REGISTRY_STORAGE", res[0].Name)
	th.AssertEquals(t, "swift", res[0].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_CONTAINER", res[1].Name)
	th.AssertEquals(t, "registry", res[1].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_AUTHURL", res[2].Name)
	th.AssertEquals(t, "http://localhost:5000/v3", res[2].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_USERNAME", res[3].Name)
	th.AssertEquals(t, true, res[3].ValueFrom != nil)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_PASSWORD", res[4].Name)
	th.AssertEquals(t, true, res[4].ValueFrom != nil)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_AUTHVERSION", res[5].Name)
	th.AssertEquals(t, "3", res[5].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_DOMAIN", res[6].Name)
	th.AssertEquals(t, domain, res[6].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_TENANT", res[7].Name)
	th.AssertEquals(t, tenant, res[7].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_REGION", res[8].Name)
	th.AssertEquals(t, "RegionOne", res[8].Value)
}

func TestSwiftEnsureAuthURLHasAPIVersion(t *testing.T) {
	configListShouldPass := []imageregistryv1.ImageRegistryConfigStorageSwift{
		{
			AuthURL:     "http://v1v2v3.com:5000/v3",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/./././",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/./././v3//",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/v3/",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000",
			AuthVersion: "2",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/v2.0",
			AuthVersion: "3",
		},
	}

	for _, config := range configListShouldPass {
		d := driver{
			Config: &config,
		}
		err := d.ensureAuthURLHasAPIVersion()
		th.AssertNoErr(t, err)
		th.AssertEquals(t, config.AuthURL, d.Config.AuthURL)
	}

	configListShouldFail := []imageregistryv1.ImageRegistryConfigStorageSwift{
		{
			AuthURL:     "http://v1v2v3.com:5000/./././v/3//",
			AuthVersion: "3",
		},
		{
			AuthURL:     "INVALID_URL",
			AuthVersion: "3",
		},
		{
			AuthURL:     "http://v1v2v3.com:5000/abracadabra",
			AuthVersion: "3",
		},
	}

	for _, config := range configListShouldFail {
		d := driver{
			Config: &config,
		}
		err := d.ensureAuthURLHasAPIVersion()
		th.AssertEquals(t, true, err != nil)
	}
}

func TestSwiftEndpointTypeObjectStore(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t, "object-store")

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "HEAD")
		th.TestHeader(t, r, "Accept", "application/json")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
		w.Header().Set("X-Container-Bytes-Used", "100")
		w.Header().Set("X-Container-Object-Count", "4")
		w.Header().Set("X-Container-Read", "test")
		w.Header().Set("X-Container-Write", "test2,user4")
		w.Header().Set("X-Timestamp", "1471298837.95721")
		w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0057b4ba37")
		w.Header().Set("X-Storage-Policy", "test_policy")
		w.WriteHeader(http.StatusNoContent)
	})

	d, installConfig := mockConfig(false, th.Endpoint()+"v3", MockUPISecretNamespaceLister{})

	res, err := d.StorageExists(&installConfig)

	th.AssertNoErr(t, err)
	th.AssertEquals(t, true, res)
}
