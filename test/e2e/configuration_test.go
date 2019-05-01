package e2e

import (
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapiv1 "github.com/openshift/api/operator/v1"

	imageregistryapiv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestPodResourceConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("512Mi"),
				},
			},
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "node-role.kubernetes.io/master",
					Operator: "Exists",
					Effect:   "NoSchedule",
				},
			},
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsSet(t, client)

	deployments, err := client.Deployments(imageregistryapiv1.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deployments.Items) == 0 {
		t.Errorf("no deployments found in registry namespace")
	}

	for _, deployment := range deployments.Items {
		if strings.HasPrefix(deployment.Name, "image-registry") {
			mem, ok := deployment.Spec.Template.Spec.Containers[0].Resources.Limits[corev1.ResourceMemory]
			if !ok {
				framework.DumpYAML(t, "deployment", deployment)
				t.Errorf("no memory limit set on registry deployment")
			}
			if mem.String() != "512Mi" {
				t.Errorf("expected memory limit of 512Mi, found: %s", mem.String())
			}
		}

	}
}

func TestPodTolerationsConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	tolerations := []corev1.Toleration{
		{
			Key:      "mykey",
			Value:    "myvalue",
			Effect:   "NoSchedule",
			Operator: "Equal",
		},
	}

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas:    1,
			Tolerations: tolerations,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	deployment, err := client.Deployments(imageregistryapiv1.ImageRegistryOperatorNamespace).Get("image-registry", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(tolerations, deployment.Spec.Template.Spec.Tolerations) {
		t.Errorf("expected tolerations not found wanted: %#v, got %#v", tolerations, deployment.Spec.Template.Spec.Tolerations)
	}

}

func TestRouteConfiguration(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	hostname := "test.example.com"

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas:     1,
			DefaultRoute: true,
			Routes: []imageregistryapiv1.ImageRegistryConfigRoute{
				{
					Name:     "testroute",
					Hostname: hostname,
				},
			},
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureDefaultExternalRegistryHostnameIsSet(t, client)
	framework.EnsureExternalRegistryHostnamesAreSet(t, client, []string{hostname})
	framework.MustEnsureDefaultExternalRouteExists(t, client)
	framework.EnsureExternalRoutesExist(t, client, []string{hostname})
}

func TestVersionReporting(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapiv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapiv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryapiv1.ImageRegistryResourceName,
		},
		Spec: imageregistryapiv1.ImageRegistrySpec{
			ManagementState: operatorapiv1.Managed,
			Storage: imageregistryapiv1.ImageRegistryConfigStorage{
				EmptyDir: &imageregistryapiv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	if _, err := client.Deployments(framework.OperatorDeploymentNamespace).Patch(framework.OperatorDeploymentName, types.StrategicMergePatchType, []byte(`{"spec": {"template": {"spec": {"containers": [{"name":"cluster-image-registry-operator","env":[{"name":"RELEASE_VERSION","value":"test-v2"}]}]}}}}`)); err != nil {
		t.Fatalf("failed to patch operator to new version: %v", err)
	}

	err := wait.Poll(5*time.Second, 1*time.Minute, func() (bool, error) {
		clusterOperatorStatus, err := client.ClusterOperators().Get(imageregistryapiv1.ImageRegistryClusterOperatorResourceName, metav1.GetOptions{})
		if err != nil {
			t.Logf("Could not retrieve cluster operator status: %v", err)
			return false, nil
		}
		if len(clusterOperatorStatus.Status.Versions) == 0 {
			// We should always have *some* version information in the clusteroperator once we are avaiable,
			// so we do not retry in this scenario.
			t.Fatalf("Cluster operator status has no version information: %v", clusterOperatorStatus)
			return true, err
		}
		if clusterOperatorStatus.Status.Versions[0].Name != "operator" || clusterOperatorStatus.Status.Versions[0].Version != "test-v2" {
			t.Logf("waiting for new version to be reported, saw: %v", clusterOperatorStatus.Status.Versions[0])
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("failed to observe updated version reported in clusteroperator status: %v", err)
	}
}
