package e2e

import (
	"context"
	"fmt"
	"regexp"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBasicEmptyDir(t *testing.T) {
	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorv1.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	defer framework.TeardownImageRegistry(te)

	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	deploy, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if deploy.Status.AvailableReplicas == 0 {
		framework.DumpObject(t, "deployment", deploy)
		t.Errorf("error: the deployment doesn't have available replicas")
	}

	logs, err := framework.GetOperatorLogs(te.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !logs.Contains(regexp.MustCompile(`Overwriting root TLS certificate authority trust store`)) {
		t.Error("error: the log doesn't contain message from the entrypoint script")
	}
	if !logs.Contains(regexp.MustCompile(`Cluster Image Registry Operator Version: .+`)) {
		t.Error("error: the log doesn't contain the operator's version")
	}
	if !logs.Contains(regexp.MustCompile(`Watching files \[/var/run/configmaps/trusted-ca/tls-ca-bundle\.pem /etc/secrets/tls\.crt /etc/secrets/tls\.key\]`)) {
		t.Error("error: the log doesn't contain correct watch files")
	}
	if !logs.Contains(regexp.MustCompile(`object changed`)) {
		t.Error("error: the log doesn't contain changes")
	}

	framework.CheckClusterOperatorCondition(te, "image-registry", configv1.OperatorUpgradeable, func(cond *configv1.ClusterOperatorStatusCondition, found bool) error {
		if !found {
			return fmt.Errorf("condition is not set")
		}
		if cond.Status != configv1.ConditionFalse {
			return fmt.Errorf("got %s, want %s", cond.Status, configv1.ConditionFalse)
		}
		return nil
	})
}
