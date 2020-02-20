package e2e

import (
	"regexp"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBasicEmptyDir(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)

	cr := &imageregistryv1.Config{
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
				EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
			},
			Replicas: 1,
		},
	}
	framework.MustDeployImageRegistry(t, client, cr)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	deploy, err := client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Status.AvailableReplicas == 0 {
		framework.DumpObject(t, "deployment", deploy)
		t.Errorf("error: the deployment doesn't have available replicas")
	}

	logs, err := framework.GetOperatorLogs(client)
	if err != nil {
		t.Fatal(err)
	}
	badlogs := false
	if !logs.Contains(regexp.MustCompile(`Cluster Image Registry Operator Version: .+`)) {
		badlogs = true
		t.Error("error: the log doesn't contain the operator's version")
	}
	if !logs.Contains(regexp.MustCompile(`object changed`)) {
		badlogs = true
		t.Error("error: the log doesn't contain changes")
	}
	if badlogs {
		framework.DumpPodLogs(t, logs)
	}
}
