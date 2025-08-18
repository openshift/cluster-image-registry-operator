package resource

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	v1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"

	cirofake "github.com/openshift/cluster-image-registry-operator/pkg/client/fake"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/emptydir"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

func buildFakeClient(config *v1.Config, nodes []*corev1.Node) *cirofake.Fixtures {
	testBuilder := cirofake.NewFixturesBuilder()
	testBuilder.AddRegistryOperatorConfig(config)

	ns := &corev1.Namespace{
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
	testBuilder.AddNamespaces(ns)
	testBuilder.AddNodes(nodes...)
	return testBuilder.Build()
}

func TestMakePodTemplateSpecWithTopologySpread(t *testing.T) {
	nodeWorkerA := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-a",
			Labels: map[string]string{
				"topology.kubernetes.io/zone":    "a",
				"kubernetes.io/hostname":         "a",
				"node-role.kubernetes.io/worker": "",
			},
		},
	}
	nodeWorkerB := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "worker-b",
			Labels: map[string]string{
				"topology.kubernetes.io/zone":    "b",
				"kubernetes.io/hostname":         "b",
				"node-role.kubernetes.io/worker": "",
			},
		},
	}
	nodeMasterA := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "master-a",
			Labels: map[string]string{
				"topology.kubernetes.io/zone":    "a",
				"kubernetes.io/hostname":         "a",
				"node-role.kubernetes.io/master": "",
			},
		},
	}
	tests := map[string]struct {
		spec     v1.ImageRegistrySpec
		nodes    []*corev1.Node
		expected []corev1.TopologySpreadConstraint
	}{
		"testSetsSaneDefaults": {
			nodes: []*corev1.Node{nodeMasterA, nodeWorkerA, nodeWorkerB},
			spec:  v1.ImageRegistrySpec{},
			expected: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "kubernetes.io/hostname",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: defaults.DeploymentLabels,
					},
				},
				{
					MaxSkew:           1,
					TopologyKey:       "node-role.kubernetes.io/worker",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: defaults.DeploymentLabels,
					},
				},
				{
					MaxSkew:           1,
					TopologyKey:       "topology.kubernetes.io/zone",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: defaults.DeploymentLabels,
					},
				},
			},
		},
		"testOmitsDefaultsWithNodeSelector": {
			nodes: []*corev1.Node{nodeMasterA, nodeWorkerA, nodeWorkerB},
			spec: v1.ImageRegistrySpec{
				NodeSelector: map[string]string{
					"node-role.kubernetes.io/master": "",
				},
			},
			expected: nil,
		},
		"testUserDefinedOverrideDefaults": {
			nodes: []*corev1.Node{nodeMasterA, nodeWorkerA, nodeWorkerB},
			spec: v1.ImageRegistrySpec{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           2,
						TopologyKey:       "topology.kubernetes.io/region",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"registry": "abc"},
						},
					},
				},
			},
			expected: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           2,
					TopologyKey:       "topology.kubernetes.io/region",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"registry": "abc"},
					},
				},
			},
		},
		"testUserDefinedEmptyOverridesDefaults": {
			nodes: []*corev1.Node{nodeMasterA, nodeWorkerA, nodeWorkerB},
			spec: v1.ImageRegistrySpec{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{},
			},
			expected: []corev1.TopologySpreadConstraint{},
		},
		"testDefaultsForNodeWithoutZone": {
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "worker",
						Labels: map[string]string{
							"kubernetes.io/hostname":         "a",
							"node-role.kubernetes.io/worker": "",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "master",
						Labels: map[string]string{
							"kubernetes.io/hostname":         "b",
							"node-role.kubernetes.io/master": "",
						},
					},
				},
			},
			spec: v1.ImageRegistrySpec{},
			expected: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "kubernetes.io/hostname",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: defaults.DeploymentLabels,
					},
				},
				{
					MaxSkew:           1,
					TopologyKey:       "node-role.kubernetes.io/worker",
					WhenUnsatisfiable: corev1.DoNotSchedule,
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: defaults.DeploymentLabels,
					},
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			config := &v1.Config{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Spec: tc.spec,
			}
			fixture := buildFakeClient(config, tc.nodes)
			emptyDirStorage := emptydir.NewDriver(&v1.ImageRegistryConfigStorageEmptyDir{})
			pod, _, err := makePodTemplateSpec(
				fixture.KubeClient.CoreV1(),
				fixture.Listers.ProxyConfigs,
				emptyDirStorage,
				config,
			)
			if err != nil {
				t.Fatalf("error creating pod template: %v", err)
			}
			expected := tc.expected
			got := pod.Spec.TopologySpreadConstraints
			if !reflect.DeepEqual(got, expected) {
				t.Logf("want: %#v", expected)
				t.Logf("got:  %#v", got)
				t.Fatalf("wrong pod template spec")
			}
		})
	}
}

type volumeMount struct {
	volExists   bool
	mountExists bool
	refName     string
	mountPath   string
	items       []corev1.KeyToPath
	optional    bool
}

func TestMakePodTemplateSpecWithVolumeMounts(t *testing.T) {
	// TODO: Make this table-driven to verify all storage drivers
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
	emptyDirStorage := emptydir.NewDriver(config.Spec.Storage.EmptyDir)
	pod, deps, err := makePodTemplateSpec(fixture.KubeClient.CoreV1(), fixture.Listers.ProxyConfigs, emptyDirStorage, config)
	if err != nil {
		t.Fatalf("error creating pod template: %v", err)
	}

	// Verify volumes and mounts
	expectedVolumes := map[string]*volumeMount{
		"ca-trust-extracted": {
			mountPath: "/etc/pki/ca-trust/extracted",
		},
		"registry-tls": {
			refName:   defaults.ImageRegistryName + "-tls",
			mountPath: "/etc/secrets",
		},
		"registry-certificates": {
			refName:   defaults.ImageRegistryCertificatesName,
			mountPath: "/etc/pki/ca-trust/source/anchors",
		},
		"trusted-ca": {
			refName:   defaults.TrustedCAName,
			mountPath: "/usr/share/pki/ca-trust-source",
			items: []corev1.KeyToPath{
				{
					Key:  "ca-bundle.crt",
					Path: "anchors/ca-bundle.crt",
				},
			},
			optional: true,
		},
		"installation-pull-secrets": {
			refName:   defaults.InstallationPullSecret,
			mountPath: "/var/lib/kubelet/",
			optional:  true,
			items: []corev1.KeyToPath{
				{
					Key:  ".dockerconfigjson",
					Path: "config.json",
				},
			},
		},
		"bound-sa-token": {
			mountPath: "/var/run/secrets/openshift/serviceaccount",
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
		"image-registry-tls":        false,
		"installation-pull-secrets": false,
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

	fsGroupChangePolicy := pod.Spec.SecurityContext.FSGroupChangePolicy
	if fsGroupChangePolicy == nil || *fsGroupChangePolicy != corev1.FSGroupChangeOnRootMismatch {
		t.Errorf("expected FSGroupChangePolicy to be set to OnRootMismatch")
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

func TestMakePodTemplateSpecS3CloudFront(t *testing.T) {
	ctx := context.Background()

	testBuilder := cirofake.NewFixturesBuilder()
	config := &v1.Config{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: v1.ImageRegistrySpec{
			Storage: v1.ImageRegistryConfigStorage{
				ManagementState: "Unmanaged",
				S3: &v1.ImageRegistryConfigStorageS3{
					Bucket:  "bucket",
					Region:  "region",
					Encrypt: true,
					CloudFront: &v1.ImageRegistryConfigStorageS3CloudFront{
						BaseURL:   "https://cloudfront.example.com",
						KeypairID: "keypair-id",
						Duration: metav1.Duration{
							Duration: 300 * time.Second,
						},
					},
					VirtualHostedStyle: true,
				},
			},
		},
	}
	testBuilder.AddRegistryOperatorConfig(config)

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
				AWS: &configv1.AWSPlatformStatus{
					Region: "region",
				},
			},
		},
	}
	testBuilder.AddInfraConfig(infra)

	imageRegNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-image-registry",
			Annotations: map[string]string{
				"openshift.io/sa.scc.supplemental-groups": "1000430000/10000",
			},
		},
	}
	testBuilder.AddNamespaces(imageRegNs)

	fixture := testBuilder.Build()
	TestFeatureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
		[]configv1.FeatureGateName{util.TestFeatureGateName},
		[]configv1.FeatureGateName{},
	)
	s3Storage := s3.NewDriver(ctx, config.Spec.Storage.S3, &fixture.Listers.StorageListers, TestFeatureGateAccessor)
	pod, _, err := makePodTemplateSpec(fixture.KubeClient.CoreV1(), fixture.Listers.ProxyConfigs, s3Storage, config)
	if err != nil {
		t.Fatalf("error creating pod template: %v", err)
	}

	ignoreEnvVar := func(name string) bool {
		return !strings.HasPrefix(name, "REGISTRY_STORAGE") && !strings.HasPrefix(name, "REGISTRY_MIDDLEWARE")
	}

	expectedEnvVars := map[string]corev1.EnvVar{
		"REGISTRY_STORAGE":                          {Value: "s3"},
		"REGISTRY_STORAGE_S3_BUCKET":                {Value: "bucket"},
		"REGISTRY_STORAGE_S3_REGION":                {Value: "region"},
		"REGISTRY_STORAGE_S3_ENCRYPT":               {Value: "true"},
		"REGISTRY_STORAGE_S3_FORCEPATHSTYLE":        {Value: "false"},
		"REGISTRY_STORAGE_S3_USEDUALSTACK":          {Value: "true"},
		"REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH": {Value: "/var/run/secrets/cloud/credentials"},
		"REGISTRY_MIDDLEWARE_STORAGE": {Value: `- name: cloudfront
  options:
    baseurl: https://cloudfront.example.com
    privatekey: /etc/docker/cloudfront/private.pem
    keypairid: keypair-id
    duration: 5m0s
    ipfilteredby: none`},
		"REGISTRY_STORAGE_CACHE_BLOBDESCRIPTOR": {Value: "inmemory"},
		"REGISTRY_STORAGE_DELETE_ENABLED":       {Value: "true"},
	}

	for _, envVar := range pod.Spec.Containers[0].Env {
		expected, ok := expectedEnvVars[envVar.Name]
		if !ok {
			if !ignoreEnvVar(envVar.Name) {
				t.Errorf("unexpected env var %s", envVar.Name)
			}
			continue
		}
		if envVar.Value != expected.Value {
			t.Errorf("expected env var %s to have value %s, got %s", envVar.Name, expectedEnvVars[envVar.Name].Value, envVar.Value)
		}
		delete(expectedEnvVars, envVar.Name)
	}
	for name := range expectedEnvVars {
		t.Errorf("expected env var %s not found", name)
	}
}
