package e2e_test

import (
	"regexp"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	imageregistryapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestBasicEmptyDir(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: testframework.ImageRegistryName,
		},
		Spec: imageregistryapi.ImageRegistrySpec{
			OperatorSpec: operatorapi.OperatorSpec{
				ManagementState: operatorapi.Managed,
			},
			Storage: imageregistryapi.ImageRegistryConfigStorage{
				Filesystem: &imageregistryapi.ImageRegistryConfigStorageFilesystem{
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Replicas: 1,
		},
	}
	testframework.MustDeployImageRegistry(t, client, cr)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)
	testframework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	testframework.MustEnsureClusterOperatorStatusIsSet(t, client)

	deploy, err := client.Deployments(testframework.ImageRegistryDeploymentNamespace).Get(testframework.ImageRegistryDeploymentName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Status.AvailableReplicas == 0 {
		testframework.DumpObject(t, "deployment", deploy)
		t.Errorf("error: the deployment doesn't have available replicas")
	}

	logs, err := testframework.GetOperatorLogs(client)
	if err != nil {
		t.Fatal(err)
	}
	badlogs := false
	if !logs.Contains(regexp.MustCompile(`Cluster Image Registry Operator Version: .+`)) {
		badlogs = true
		t.Error("error: the log doesn't contain the operator's version")
	}
	if !logs.Contains(regexp.MustCompile(`status changed`)) {
		badlogs = true
		t.Error("error: the log doesn't contain changes")
	}
	if badlogs {
		testframework.DumpPodLogs(t, logs)
	}
}
