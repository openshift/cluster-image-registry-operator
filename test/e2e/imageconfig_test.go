package e2e

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestAdditionalTrustedCA(t *testing.T) {
	const openshiftConfigNamespace = "openshift-config"
	const imageConfigName = "cluster"
	const userCAConfigMapName = "test-image-registry-operator-additional-trusted-ca"
	const imageRegistryCAConfigMapName = "image-registry-certificates"

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	caData := map[string]string{
		"foo.example.com":       "certificateFoo",
		"bar.example.com..5000": "certificateBar",
	}

	err := te.Client().ConfigMaps(openshiftConfigNamespace).Delete(userCAConfigMapName, &metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		t.Fatal(err)
	}

	_, err = te.Client().ConfigMaps(openshiftConfigNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: userCAConfigMapName,
		},
		Data: caData,
	})
	if err != nil {
		t.Fatal(err)
	}

	imageConfig, err := te.Client().Images().Get(imageConfigName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("unable to get image config: %v", err)
	}

	oldAdditionalTrustedCA := imageConfig.Spec.AdditionalTrustedCA.Name
	if _, err := te.Client().Images().Patch(imageConfigName, types.MergePatchType, []byte(`{"spec": {"additionalTrustedCA": {"name": "`+userCAConfigMapName+`"}}}`)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := te.Client().Images().Patch(imageConfigName, types.MergePatchType, []byte(`{"spec": {"additionalTrustedCA": {"name": "`+oldAdditionalTrustedCA+`"}}}`)); err != nil {
			panic(fmt.Errorf("unable to restore image config"))
		}
	}()

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas: 1,
	})
	framework.EnsureImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	certs, err := te.Client().ConfigMaps(defaults.ImageRegistryOperatorNamespace).Get(imageRegistryCAConfigMapName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range caData {
		if certs.Data[k] != v {
			t.Errorf("bad certificate: key %q: got %q, want %q", k, certs.Data[k], v)
		}
	}
	for _, k := range []string{
		"image-registry.openshift-image-registry.svc..5000",
		"image-registry.openshift-image-registry.svc.cluster.local..5000",
	} {
		if certs.Data[k] == "" {
			t.Errorf("bad certificate: key %q: got %q, want generated certificate", k, certs.Data[k])
		}
	}
	if t.Failed() {
		framework.DumpYAML(t, imageRegistryCAConfigMapName, certs)
	}
}

func TestSwapStorage(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	framework.DeployImageRegistry(te, nil)
	framework.EnsureImageRegistryIsAvailable(te)

	config, err := te.Client().Configs().Get(
		defaults.ImageRegistryResourceName,
		metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal("unable to get image registry config")
	}

	// as our tests run over IPI this should never be the case.
	if config.Status.Storage.EmptyDir != nil {
		t.Fatal("already using EmptyDir, unable to test")
	}

	config.Spec.Storage = imageregistryv1.ImageRegistryConfigStorage{
		EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
	}

	if _, err = te.Client().Configs().Update(config); err != nil {
		t.Fatal("unable to update image registry config")
	}

	// give some room for the operator to act.
	framework.EnsureImageRegistryIsAvailable(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	if config, err = te.Client().Configs().Get(
		defaults.ImageRegistryResourceName,
		metav1.GetOptions{},
	); err != nil {
		t.Fatal("unable to get image registry config")
	}

	if config.Status.Storage.EmptyDir == nil {
		t.Fatal("emptyDir storage not set")
	}

	expected := imageregistryv1.ImageRegistryConfigStorage{
		EmptyDir: config.Status.Storage.EmptyDir,
	}
	if config.Status.Storage != expected {
		t.Errorf("multi storage config found: %+v", config.Status.Storage)
	}
}
