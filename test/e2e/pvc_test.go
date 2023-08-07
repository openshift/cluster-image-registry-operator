package e2e_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func dumpResourcesOnFailure(te framework.TestEnv) {
	if te.Failed() {
		scList, err := te.Client().StorageClasses().List(
			context.Background(), metav1.ListOptions{},
		)
		if err != nil {
			te.Logf("unable to dump the storage classes: %s", err)
		} else {
			framework.DumpYAML(te, "storageclasses", scList)
		}

		pvList, err := te.Client().PersistentVolumes().List(
			context.Background(), metav1.ListOptions{},
		)
		if err != nil {
			te.Logf("unable to dump the persistent volumes: %s", err)
		} else {
			framework.DumpYAML(te, "persistentvolumes", pvList)
		}

		pvcList, err := te.Client().PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).List(
			context.Background(), metav1.ListOptions{},
		)
		if err != nil {
			te.Logf("unable to dump the persistent volume claims: %s", err)
		} else {
			framework.DumpYAML(te, "persistentvolumeclaims", pvcList)
		}
	}
}

func createPV(te framework.TestEnv, storageClass string) {
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

		_, err := te.Client().PersistentVolumes().Create(
			context.Background(), pv, metav1.CreateOptions{},
		)
		if err == nil {
			te.Logf("PersistentVolume %s created", pvName)
			name = pvName
			break
		}

		if errors.IsAlreadyExists(err) {
			continue
		}

		te.Logf("unable to create PersistentVolume %s: %s", pvName, err)
	}

	if name == "" {
		te.Fatal("unable to create PersistentVolume")
	}

	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, framework.AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			pv, pvErr := te.Client().PersistentVolumes().Get(
				ctx, name, metav1.GetOptions{},
			)
			return (pv != nil && pv.Status.Phase != corev1.VolumePending), pvErr
		},
	)
	if err != nil {
		te.Fatal(err)
	}
}

func createPVWithStorageClass(te framework.TestEnv) {
	storageClassList, err := te.Client().StorageClasses().List(
		context.Background(), metav1.ListOptions{},
	)
	if err != nil {
		te.Fatalf("unable to list storage classes: %s", err)
	}

	for _, storageClass := range storageClassList.Items {
		createPV(te, storageClass.Name)
	}
}

func createPVC(te framework.TestEnv, name string, accessMode corev1.PersistentVolumeAccessMode) {
	claim := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				accessMode,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("1Gi"),
				},
			},
		},
	}

	_, err := te.Client().PersistentVolumeClaims(defaults.ImageRegistryOperatorNamespace).Create(
		context.Background(), claim, metav1.CreateOptions{},
	)
	if err != nil {
		te.Fatal(err)
	}
}

func checkTestResult(te framework.TestEnv) {
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		te.Fatal(err)
	}
	if deploy.Status.AvailableReplicas == 0 {
		framework.DumpYAML(te, "deployment", deploy)
		te.Errorf("error: the deployment doesn't have available replicas")
	}
}

func TestDefaultPVC(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)
	defer dumpResourcesOnFailure(te)

	createPV(te, "")
	createPVWithStorageClass(te)

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{},
		},
		Replicas: 1,
	})

	checkTestResult(te)
}

func TestCustomRWXPVC(t *testing.T) {
	claimName := "test-custom-rwx-" + uuid.New().String()

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)
	defer dumpResourcesOnFailure(te)

	createPV(te, "")
	createPVWithStorageClass(te)
	createPVC(te, claimName, corev1.ReadWriteMany)

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{
				Claim: claimName,
			},
		},
		Replicas: 1,
	})

	checkTestResult(te)
}

func TestCustomRWOPVC(t *testing.T) {
	claimName := "test-custom-rwo-" + uuid.New().String()

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)
	defer dumpResourcesOnFailure(te)

	createPV(te, "")
	createPVWithStorageClass(te)
	createPVC(te, claimName, corev1.ReadWriteOnce)

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		OperatorSpec: operatorv1.OperatorSpec{
			ManagementState: operatorv1.Managed,
		},
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			PVC: &imageregistryv1.ImageRegistryConfigStoragePVC{
				Claim: claimName,
			},
		},
		Replicas:        1,
		RolloutStrategy: string(appsapi.RecreateDeploymentStrategyType),
	})

	checkTestResult(te)
}
