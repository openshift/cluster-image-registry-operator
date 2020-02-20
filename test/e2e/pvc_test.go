package e2e_test

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func testDefer(t *testing.T, client *framework.Clientset) {
	if t.Failed() {
		scList, err := client.StorageClasses().List(metav1.ListOptions{})
		if err != nil {
			t.Logf("unable to dump the storage classes: %s", err)
		} else {
			framework.DumpYAML(t, "storageclasses", scList)
		}

		pvList, err := client.PersistentVolumes().List(metav1.ListOptions{})
		if err != nil {
			t.Logf("unable to dump the persistent volumes: %s", err)
		} else {
			framework.DumpYAML(t, "persistentvolumes", pvList)
		}

		pvcList, err := client.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
		if err != nil {
			t.Logf("unable to dump the persistent volume claims: %s", err)
		} else {
			framework.DumpYAML(t, "persistentvolumeclaims", pvcList)
		}
	}
	framework.MustRemoveImageRegistry(t, client)
}

func createPV(t *testing.T, storageClass string) error {
	client := framework.MustNewClientset(t, nil)
	name := ""

	for i := 0; i < 100; i++ {
		pvName := fmt.Sprintf("pv-%s", rand.String(64))
		localPath := fmt.Sprintf("/tmp/%s", pvName)

		pv := &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{
				Name: pvName,
			},
			Spec: corev1.PersistentVolumeSpec{
				Capacity: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("100Gi"),
				},
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
					corev1.ReadOnlyMany,
					corev1.ReadWriteMany,
				},
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{
						Path: localPath,
					},
				},
				StorageClassName: storageClass,
			},
		}

		_, err := client.PersistentVolumes().Create(pv)
		if err == nil {
			t.Logf("PersistentVolume %s created", pvName)
			name = pvName
			break
		}

		if errors.IsAlreadyExists(err) {
			continue
		}

		t.Logf("unable to create PersistentVolume %s: %s", pvName, err)
	}

	if name == "" {
		return fmt.Errorf("unable to create PersistentVolume")
	}

	err := wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (bool, error) {
		pv, pvErr := client.PersistentVolumes().Get(name, metav1.GetOptions{})
		return (pv != nil && pv.Status.Phase != corev1.VolumePending), pvErr
	})

	if err != nil {
		return err
	}

	return nil
}

func createPVWithStorageClass(t *testing.T) error {
	client := framework.MustNewClientset(t, nil)

	storageClassList, err := client.StorageClasses().List(metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("unable to list storage classes: %s", err)
	}

	for _, storageClass := range storageClassList.Items {
		if err := createPV(t, storageClass.Name); err != nil {
			return err
		}
	}

	return nil
}

func createPVC(t *testing.T, name string) error {
	client := framework.MustNewClientset(t, nil)

	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteMany,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}

	_, err := client.PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Create(claim)
	if err != nil {
		return err
	}

	return nil
}

func checkTestResult(t *testing.T, client *framework.Clientset) {
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	deploy, err := client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Status.AvailableReplicas == 0 {
		framework.DumpYAML(t, "deployment", deploy)
		t.Errorf("error: the deployment doesn't have available replicas")
	}
}

func TestDefaultPVC(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer testDefer(t, client)

	if err := createPV(t, ""); err != nil {
		t.Fatal(err)
	}

	if err := createPVWithStorageClass(t); err != nil {
		t.Fatal(err)
	}

	framework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapi.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{},
			},
			Replicas: 1,
		},
	})

	checkTestResult(t, client)
}

func TestCustomPVC(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer testDefer(t, client)

	if err := createPV(t, ""); err != nil {
		t.Fatal(err)
	}

	if err := createPVWithStorageClass(t); err != nil {
		t.Fatal(err)
	}

	if err := createPVC(t, "test-custom-pvc"); err != nil {
		t.Fatal(err)
	}

	framework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: defaults.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapi.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{
					Claim: "test-custom-pvc",
				},
			},
			Replicas: 1,
		},
	})

	checkTestResult(t, client)
}
