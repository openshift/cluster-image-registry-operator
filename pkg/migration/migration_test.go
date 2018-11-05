package migration_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/apimachinery/pkg/util/yaml"

	appsapi "github.com/openshift/api/apps/v1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/migration"
)

func resourceFromFile(out interface{}, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := yaml.NewYAMLOrJSONDecoder(f, 100).Decode(out); err != nil {
		return fmt.Errorf("load resource from file %s: %s", filename, err)
	}
	return nil
}

type mockResources struct {
	secret    func(name string) (*corev1.Secret, error)
	configMap func(name string) (*corev1.ConfigMap, error)
}

func (m *mockResources) Secret(name string) (*corev1.Secret, error) {
	return m.secret(name)
}

func (m *mockResources) ConfigMap(name string) (*corev1.ConfigMap, error) {
	return m.configMap(name)
}

func TestNewImageRegistrySpecFromDeploymentConfig(t *testing.T) {
	tests, err := ioutil.ReadDir("./testdata")
	if err != nil {
		t.Fatalf("unable to list testdata: %s", err)
	}

	for _, test := range tests {
		for _, env := range os.Environ() {
			envParts := strings.SplitN(env, "=", 2)
			if strings.HasPrefix(envParts[0], "REGISTRY_") {
				os.Unsetenv(envParts[0])
			}
		}

		testName := test.Name()
		t.Run(testName, func(t *testing.T) {
			var dc appsapi.DeploymentConfig
			if err := resourceFromFile(&dc, "./testdata/"+testName+"/deploymentconfig.yaml"); err != nil {
				t.Fatal(err)
			}

			var expectedSpec imageregistryapi.ImageRegistrySpec
			if err := resourceFromFile(&expectedSpec, "./testdata/"+testName+"/imageregistryspec.yaml"); err != nil {
				t.Fatal(err)
			}

			spec, tlsSecret, err := migration.NewImageRegistrySpecFromDeploymentConfig(&dc, &mockResources{
				secret: func(name string) (*corev1.Secret, error) {
					var secret corev1.Secret
					return &secret, resourceFromFile(&secret, "./testdata/"+testName+"/secret-"+name+".yaml")
				},
				configMap: func(name string) (*corev1.ConfigMap, error) {
					var configMap corev1.ConfigMap
					return &configMap, resourceFromFile(&configMap, "./testdata/"+testName+"/configmap-"+name+".yaml")
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			if !reflect.DeepEqual(spec, expectedSpec) {
				t.Errorf("got different image registry spec (A - actual, B - expected): %s", diff.ObjectGoPrintDiff(spec, expectedSpec))
			}
			tlsSecret = tlsSecret // TODO
		})
	}
}
