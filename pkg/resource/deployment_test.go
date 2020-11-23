package resource

import (
	"context"
	"reflect"
	"testing"
	"time"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	kubeclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	fakeconfig "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/envvar"
)

func TestChecksum(t *testing.T) {
	tests := []struct {
		name          string
		origSecret    *corev1.Secret
		updatedSecret *corev1.Secret
		validate      func(*testing.T, string, *appsapi.Deployment)
	}{
		{
			name: "no secret changes",
			origSecret: testSecret(map[string][]byte{
				"credentials": []byte("orig creds"),
			}),
			validate: func(t *testing.T, origHash string, dep *appsapi.Deployment) {
				currentHash := dep.Annotations[defaults.ChecksumOperatorAnnotation]
				if origHash != currentHash {
					t.Errorf("Has unexpectedly changed from %s to %s", origHash, currentHash)
				}
			},
		},
		{
			name: "secret changes",
			origSecret: testSecret(map[string][]byte{
				"credentials": []byte("orig creds"),
			}),
			updatedSecret: testSecret(map[string][]byte{
				"credentials": []byte("updated creds"),
			}),
			validate: func(t *testing.T, origHash string, dep *appsapi.Deployment) {
				currentHash := dep.Annotations[defaults.ChecksumOperatorAnnotation]
				if origHash == currentHash {
					t.Errorf("Hash unexpectedly didn't change from %s", origHash)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			annotatedNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: defaults.ImageRegistryOperatorNamespace,
					Annotations: map[string]string{
						defaults.SupplementalGroupsAnnotation: "1/2",
					},
				},
			}

			imageRegistryCertificatesConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      defaults.ImageRegistryCertificatesName,
					Namespace: defaults.ImageRegistryOperatorNamespace,
				},
			}

			kubeClient := fake.NewSimpleClientset(test.origSecret, annotatedNamespace, imageRegistryCertificatesConfigMap)
			kubeInformer := kubeinformers.NewSharedInformerFactory(kubeClient, 0)

			configClient := fakeconfig.NewSimpleClientset()
			configInformer := configinformers.NewSharedInformerFactory(configClient, 0)

			cmLister := kubeInformer.Core().V1().ConfigMaps().Lister().ConfigMaps(defaults.ImageRegistryOperatorNamespace)
			secretLister := kubeInformer.Core().V1().Secrets().Lister().Secrets(defaults.ImageRegistryOperatorNamespace)

			proxyLister := configInformer.Config().V1().Proxies().Lister()

			kubeInformer.Start(ctx.Done())
			configInformer.Start(ctx.Done())

			gd := &generatorDeployment{
				driver:          &testDriver{},
				coreClient:      kubeClient.CoreV1(),
				proxyLister:     proxyLister,
				cr:              &imageregistryv1.Config{},
				configMapLister: cmLister,
				secretLister:    secretLister,
			}

			cache.WaitForCacheSync(ctx.Done(), kubeInformer.Core().V1().ConfigMaps().Informer().HasSynced)
			cache.WaitForCacheSync(ctx.Done(), kubeInformer.Core().V1().Secrets().Informer().HasSynced)

			// Generate a base deployment
			obj, err := gd.expected()
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			generatedDeployment, ok := obj.(*appsapi.Deployment)
			if !ok {
				t.Fatalf("failed to convert to Deployment")
			}

			origHash := generatedDeployment.Annotations[defaults.ChecksumOperatorAnnotation]

			if test.updatedSecret != nil {
				_, err = kubeClient.CoreV1().Secrets(defaults.ImageRegistryOperatorNamespace).Update(ctx, test.updatedSecret, metav1.UpdateOptions{})
				if err != nil {
					t.Fatalf("failed to update secret: %s", err)
				}

				cache.WaitForCacheSync(ctx.Done(), kubeInformer.Core().V1().Secrets().Informer().HasSynced)
				// Fixme: why is this wait necessary?
				if err := waitForUpdatedSecret(ctx, kubeClient, test.updatedSecret.Data); err != nil {
					t.Fatalf("failed waiting for secret update to become visible: %s", err)
				}
			}

			// Check for expected changes in the deployment
			obj, err = gd.expected()
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			regeneratedDeployment, ok := obj.(*appsapi.Deployment)
			if !ok {
				t.Fatalf("failed to convert to Deployment")
			}

			test.validate(t, origHash, regeneratedDeployment)
		})
	}
}

func testSecret(sData map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: defaults.ImageRegistryOperatorNamespace,
			Name:      defaults.ImageRegistryPrivateConfiguration,
		},
		Data: sData,
	}
}

func waitForUpdatedSecret(ctx context.Context, kubeClient kubeclient.Interface, expectedData map[string][]byte) error {
	err := wait.Poll(time.Second, time.Minute, func() (stop bool, err error) {
		sec, err := kubeClient.CoreV1().Secrets(defaults.ImageRegistryOperatorNamespace).Get(ctx, defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
		if err != nil {
			// Keep waiting
			return false, nil
		}
		if reflect.DeepEqual(sec.Data, expectedData) {
			return true, nil
		}
		// Keep trying
		return false, nil
	})
	return err
}

type testDriver struct{}

func (d *testDriver) ConfigEnv() (envvar.List, error) {
	return nil, nil
}

func (d *testDriver) CreateStorage(*imageregistryv1.Config) error {
	panic("CreateStorage not implemented")
}

func (d *testDriver) ID() string {
	panic("ID not implemented")
}

func (d *testDriver) RemoveStorage(*imageregistryv1.Config) (bool, error) {
	panic("RemoveStorage not implemented")
}

func (d *testDriver) StorageChanged(*imageregistryv1.Config) bool {
	panic("StorageChanged not implemented")
}

func (d *testDriver) StorageExists(*imageregistryv1.Config) (bool, error) {
	panic("StorageExists not implemented")
}

func (d *testDriver) VolumeSecrets() (map[string]string, error) {
	panic("VolumeSecrets not implemented")
}

func (d *testDriver) Volumes() ([]corev1.Volume, []corev1.VolumeMount, error) {
	volumes := []corev1.Volume{
		{
			Name: defaults.ImageRegistryPrivateConfiguration,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: defaults.ImageRegistryPrivateConfiguration,
				},
			},
		},
	}
	return volumes, []corev1.VolumeMount{}, nil
}
