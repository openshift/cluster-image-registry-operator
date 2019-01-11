package e2e_test

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestConfiguration(t *testing.T) {
	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryv1.Config{
		TypeMeta: metav1.TypeMeta{
			APIVersion: imageregistryv1.SchemeGroupVersion.String(),
			Kind:       "Config",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: imageregistryv1.ImageRegistryResourceName,
		},
		Spec: imageregistryv1.ImageRegistrySpec{
			ManagementState: operatorapi.Managed,
			Storage: imageregistryv1.ImageRegistryConfigStorage{
				Filesystem: &imageregistryv1.ImageRegistryConfigStorageFilesystem{
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Replicas: 1,
			Resources: &corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					"memory": resource.MustParse("512Mi"),
				},
			},
			NodeSelector: map[string]string{
				"node-role.kubernetes.io/master": "",
			},
		},
	}
	testframework.MustDeployImageRegistry(t, client, cr)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)
	testframework.MustEnsureClusterOperatorStatusIsSet(t, client)

	pods, err := client.Pods(imageregistryv1.ImageRegistryOperatorNamespace).List(metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods.Items) == 0 {
		t.Errorf("no pods found in registry namespace")
	}

	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, "image-registry") {
			mem, ok := pod.Spec.Containers[0].Resources.Limits["memory"]
			if !ok {
				t.Errorf("no memory limit set on registry pod: %#v", pod)
			}
			if mem.String() != "512Mi" {
				t.Errorf("expected memory limit of 512Mi, found: %s", mem.String())
			}
		}
	}

}
