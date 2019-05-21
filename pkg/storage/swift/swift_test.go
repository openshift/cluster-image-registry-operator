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

	operatorapi "github.com/openshift/api/operator/v1"
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

func handleAuthentication(t *testing.T) {
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
					"type": "container",
					"name": "swift"
				}]
			}
		}`)
	})
}

func TestSwiftCreateStorageNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t)

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "PUT")
		th.TestHeader(t, r, "Accept", "application/json")

		w.Header().Set("Content-Length", "0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
		w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0058b4ba37")
		w.WriteHeader(http.StatusNoContent)
	})

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		AuthURL:   th.Endpoint() + "v3",
		Container: container,
		Domain:    domain,
		Tenant:    tenant,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockUPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
	}

	d.CreateStorage(&installConfig)

	th.AssertEquals(t, true, installConfig.Status.StorageManaged)
	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionTrue, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, container, installConfig.Status.Storage.Swift.Container)
}

func TestSwiftRemoveStorageNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t)

	th.Mux.HandleFunc("/"+container, func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "DELETE")
		th.TestHeader(t, r, "Accept", "application/json")
		w.WriteHeader(http.StatusNoContent)
	})

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		AuthURL:   th.Endpoint() + "v3",
		Container: container,
		Domain:    domain,
		Tenant:    tenant,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockUPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
		Status: imageregistryv1.ImageRegistryStatus{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
			StorageManaged: true,
		},
	}

	d.RemoveStorage(&installConfig)

	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionFalse, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, "", installConfig.Status.Storage.Swift.Container)
}

func TestSwiftStorageExistsNativeSecret(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t)

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

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		AuthURL:   th.Endpoint() + "v3",
		Container: container,
		Domain:    domain,
		Tenant:    tenant,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockUPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
	}

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
			Secrets: MockUPISecretNamespaceLister{},
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
	d = driver{
		Listers: &regopclient.Listers{
			Secrets: MockIPISecretNamespaceLister{},
		},
		Config: &config,
	}
	fakeCloudsYAMLData := []byte(`clouds:
  ` + cloudName + `:
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
	handleAuthentication(t)

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
		th.TestMethod(t, r, "PUT")
		th.TestHeader(t, r, "Accept", "application/json")

		w.Header().Set("Content-Length", "0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		w.Header().Set("Date", "Wed, 17 Aug 2016 19:25:43 GMT")
		w.Header().Set("X-Trans-Id", "tx554ed59667a64c61866f1-0058b4ba37")
		w.WriteHeader(http.StatusNoContent)
	})

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		Container: container,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockIPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
	}

	d.CreateStorage(&installConfig)

	th.AssertEquals(t, true, installConfig.Status.StorageManaged)
	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionTrue, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, container, installConfig.Status.Storage.Swift.Container)
}

func TestSwiftRemoveStorageCloudConfig(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t)

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

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		Container: container,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockIPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
		Status: imageregistryv1.ImageRegistryStatus{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
			StorageManaged: true,
		},
	}

	d.RemoveStorage(&installConfig)

	th.AssertEquals(t, "StorageExists", installConfig.Status.Conditions[0].Type)
	th.AssertEquals(t, operatorapi.ConditionFalse, installConfig.Status.Conditions[0].Status)
	th.AssertEquals(t, "", installConfig.Status.Storage.Swift.Container)
}

func TestSwiftStorageExistsCloudConfig(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	handleAuthentication(t)

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

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		Container: container,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockIPISecretNamespaceLister{},
		},
		Config: &config,
	}

	installConfig := imageregistryv1.Config{
		Spec: imageregistryv1.ImageRegistrySpec{
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Swift: &config,
			},
		},
	}

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

	config := imageregistryv1.ImageRegistryConfigStorageSwift{
		Container: container,
	}
	d := driver{
		Listers: &regopclient.Listers{
			Secrets: MockIPISecretNamespaceLister{},
		},
		Config: &config,
	}

	res, err := d.ConfigEnv()

	th.AssertNoErr(t, err)
	th.AssertEquals(t, "REGISTRY_STORAGE", res[0].Name)
	th.AssertEquals(t, "swift", res[0].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_CONTAINER", res[1].Name)
	th.AssertEquals(t, "registry", res[1].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_AUTHURL", res[2].Name)
	th.AssertEquals(t, "http://localhost:5000/v3", res[2].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_DOMAIN", res[3].Name)
	th.AssertEquals(t, domain, res[3].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_DOMAINID", res[4].Name)
	th.AssertEquals(t, "", res[4].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_TENANT", res[5].Name)
	th.AssertEquals(t, tenant, res[5].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_TENANTID", res[6].Name)
	th.AssertEquals(t, "", res[6].Value)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_USERNAME", res[7].Name)
	th.AssertEquals(t, true, res[7].ValueFrom != nil)
	th.AssertEquals(t, "REGISTRY_STORAGE_SWIFT_PASSWORD", res[8].Name)
	th.AssertEquals(t, true, res[8].ValueFrom != nil)
}
