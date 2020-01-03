package resource

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
)

type volumeMount struct {
	volExists   bool
	mountExists bool
	refName     string
	mountPath   string
	items       []corev1.KeyToPath
	optional    bool
}

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

	// Verify volumes and mounts
	expectedVolumes := map[string]*volumeMount{
		"registry-tls": {
			refName:   defaults.ImageRegistryName + "-tls",
			mountPath: "/etc/secrets",
		},
		"registry-certificates": {
			refName:   defaults.ImageRegistryCertificatesName,
			mountPath: "/etc/pki/ca-trust/source/anchors",
		},
		"trusted-ca": {
			refName:   params.TrustedCA.Name,
			mountPath: "/usr/share/pki/ca-trust-source",
			items: []corev1.KeyToPath{
				{
					Key:  "ca-bundle.crt",
					Path: "anchors/ca-bundle.crt",
				},
			},
			optional: true,
		},
	}
	// emptyDir adds an additional volume
	expectedVolumes["registry-storage"] = &volumeMount{
		mountPath: "/registry",
	}
	// Verify volume mounts
	for _, v := range pod.Spec.Volumes {
		vol, ok := expectedVolumes[v.Name]
		if !ok {
			t.Errorf("volume %s was not expected", v.Name)
		} else {
			vol.volExists = true
			verifyVolume(v, vol, t)
		}
	}

	// Verify registry mounts
	registrySpec := pod.Spec.Containers[0]
	for _, v := range registrySpec.VolumeMounts {
		mount, ok := expectedVolumes[v.Name]
		if !ok {
			t.Errorf("volume mount %s was not expected", v.Name)
		} else {
			mount.mountExists = true
			verifyMount(v, mount, t)
		}
	}

	// check volumes - exist and mounted
	for name, vol := range expectedVolumes {
		if !vol.volExists {
			t.Errorf("volume %s was not found", name)
		}
		if !vol.mountExists {
			t.Errorf("volume %s was not mounted", name)
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

func verifyVolume(volume corev1.Volume, expected *volumeMount, t *testing.T) {
	if volume.ConfigMap != nil {
		if volume.ConfigMap.LocalObjectReference.Name != expected.refName {
			t.Errorf("expected volume %s to reference ConfigMap %s, got %s", volume.Name, expected.refName, volume.ConfigMap.LocalObjectReference.Name)
		}
		if expected.optional {
			if volume.ConfigMap.Optional == nil || *volume.ConfigMap.Optional != expected.optional {
				t.Errorf("expected volume %s to be optional=%t, got %t", volume.Name, expected.optional, *volume.ConfigMap.Optional)
			}
		}
		if len(expected.items) != len(volume.ConfigMap.Items) {
			t.Errorf("expected volume %s to mount %d items, got %d", volume.Name, len(expected.items), len(volume.ConfigMap.Items))
		}
		for i, mnt := range expected.items {
			actual := volume.ConfigMap.Items[i]
			if mnt.Key != actual.Key || mnt.Path != actual.Path {
				t.Errorf("expected volume %s to mount %s -> %s, got %s -> %s", volume.Name, mnt.Key, mnt.Path, actual.Key, actual.Path)
			}
		}
	}
	if volume.Secret != nil {
		if volume.Secret.SecretName != expected.refName {
			t.Errorf("expected volume %s to reference Secret %s, got %s", volume.Name, "trusted-ca", volume.Secret.SecretName)
		}
		if expected.optional {
			if volume.Secret.Optional == nil || *volume.Secret.Optional != expected.optional {
				t.Errorf("expected volume %s to be optional=%t, got %t", volume.Name, expected.optional, *volume.Secret.Optional)
			}
		}
		if len(expected.items) != len(volume.Secret.Items) {
			t.Errorf("expected volume %s to mount %d items, got %d", volume.Name, len(expected.items), len(volume.Secret.Items))
		}
		for i, mnt := range expected.items {
			actual := volume.Secret.Items[i]
			if mnt.Key != actual.Key || mnt.Path != actual.Path {
				t.Errorf("expected volume %s to mount %s -> %s, got %s -> %s", volume.Name, mnt.Key, mnt.Path, actual.Key, actual.Path)
			}
		}
	}
	if volume.Projected != nil && len(volume.Projected.Sources) > 0 {
		for _, src := range volume.Projected.Sources {
			if src.ConfigMap != nil {
				if src.ConfigMap.LocalObjectReference.Name != expected.refName {
					t.Errorf("expected volume %s to reference ConfigMap %s, got %s", volume.Name, expected.refName, src.ConfigMap.LocalObjectReference.Name)
				}
			}
			if src.Secret != nil {
				if src.Secret.LocalObjectReference.Name != expected.refName {
					t.Errorf("expected volume %s to reference Secret %s, got %s", volume.Name, expected.refName, src.Secret.LocalObjectReference.Name)
				}
			}
		}
	}
}

func verifyMount(mount corev1.VolumeMount, expected *volumeMount, t *testing.T) {
	if mount.MountPath != expected.mountPath {
		t.Errorf("expected mount path to be %s, got %s", expected.mountPath, mount.MountPath)
	}
}
