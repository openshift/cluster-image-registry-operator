package resource

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
)

func TestMakePodTemplateSpec(t *testing.T) {
	// TODO: Make this table-driven to verify all storage drivers
	params := &parameters.Globals{}
	params.Deployment.Namespace = "openshift-image-registry"
	params.TrustedCA.Name = "trusted-ca"

	testBuilder := cirofake.NewFixturesBuilder()
	config := &v1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: v1.ImageRegistrySpec{
			Storage: v1.ImageRegistryConfigStorage{
				EmptyDir: &v1.ImageRegistryConfigStorageEmptyDir{},
			},
		},
	}
	testBuilder.AddRegistryOperatorConfig(config)

	imageRegNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-image-registry",
			Annotations: map[string]string{
				"openshift.io/node-selector":              "",
				"openshift.io/sa.scc.supplemental-groups": "1000430000/10000",
			},
			Labels: map[string]string{
				"openshift.io/cluster-monitoring": "true",
			},
		},
		Spec: corev1.NamespaceSpec{
			Finalizers: []corev1.FinalizerName{
				corev1.FinalizerKubernetes,
			},
		},
	}
	testBuilder.AddNamespaces(imageRegNs)

	fixture := testBuilder.Build()
	emptyDirStorage := emptydir.NewDriver(config.Spec.Storage.EmptyDir, fixture.Listers)
	pod, deps, err := makePodTemplateSpec(fixture.KubeClient.CoreV1(), fixture.Listers.ProxyConfigs, emptyDirStorage, params, config)
	if err != nil {
		t.Fatalf("error creating pod template: %v", err)
	}

	expectedVolumes := map[string]bool{
		"registry-tls":          false,
		"registry-certificates": false,
		"trusted-ca":            false,
	}
	// emptyDir adds an additional volume
	expectedVolumes["registry-storage"] = false
	// Verify volume mounts
	for _, v := range pod.Spec.Volumes {
		if _, ok := expectedVolumes[v.Name]; !ok {
			t.Errorf("volume %s was not expected", v.Name)
		} else {
			expectedVolumes[v.Name] = true
		}
	}
	for vol, found := range expectedVolumes {
		if !found {
			t.Errorf("volume %s was not found", vol)
		}
	}

	// Verify dependencies
	expectedConfigMaps := map[string]bool{
		"trusted-ca":                  false,
		"image-registry-certificates": false,
	}
	expectedSecrets := map[string]bool{
		"image-registry-tls": false,
	}
	for cm := range deps.configMaps {
		if _, ok := expectedConfigMaps[cm]; !ok {
			t.Errorf("unexpected dependent ConfigMap %s", cm)
		} else {
			expectedConfigMaps[cm] = true
		}
	}
	for secret := range deps.secrets {
		if _, ok := expectedSecrets[secret]; !ok {
			t.Errorf("unexpected dependent Secret %s", secret)
		} else {
			expectedSecrets[secret] = true
		}
	}
	for cm, found := range expectedConfigMaps {
		if !found {
			t.Errorf("ConfigMap %s was not listed as a dependency", cm)
		}
	}
	for secret, found := range expectedSecrets {
		if !found {
			t.Errorf("Secret %s was not listed as a dependency", secret)
		}
	}

}
