package e2e

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	operatorapi "github.com/openshift/api/operator/v1"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestAdditionalTrustedCA(t *testing.T) {
	const openshiftConfigNamespace = "openshift-config"
	const imageConfigName = "cluster"
	const userCAConfigMapName = "test-image-registry-operator-additional-trusted-ca"
	const imageRegistryCAConfigMapName = "image-registry-certificates"
	client := framework.MustNewClientset(t, nil)

	caData := map[string]string{
		"foo.example.com":       "certificateFoo",
		"bar.example.com..5000": "certificateBar",
	}

	client.ConfigMaps(openshiftConfigNamespace).Delete(userCAConfigMapName, &metav1.DeleteOptions{})
	_, err := client.ConfigMaps(openshiftConfigNamespace).Create(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: userCAConfigMapName,
		},
		Data: caData,
	})
	if err != nil {
		t.Fatal(err)
	}

	imageConfig, err := client.Images().Get(imageConfigName, metav1.GetOptions{})
	oldAdditionalTrustedCA := imageConfig.Spec.AdditionalTrustedCA.Name
	if _, err := client.Images().Patch(imageConfigName, types.MergePatchType, []byte(`{"spec": {"additionalTrustedCA": {"name": "`+userCAConfigMapName+`"}}}`)); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := client.Images().Patch(imageConfigName, types.MergePatchType, []byte(`{"spec": {"additionalTrustedCA": {"name": "`+oldAdditionalTrustedCA+`"}}}`)); err != nil {
			panic(fmt.Errorf("unable to restore image config"))
		}
	}()

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, &imageregistryv1.Config{
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
	})
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsSet(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	defer func() {
		if t.Failed() {
			framework.DumpImageRegistryResource(t, client)
			framework.DumpOperatorLogs(t, client)
		}
	}()

	certs, err := client.ConfigMaps(defaults.ImageRegistryOperatorNamespace).Get(imageRegistryCAConfigMapName, metav1.GetOptions{})
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
	client := framework.MustNewClientset(t, nil)
	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)

	config, err := client.Configs().Get(
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

	if _, err = client.Configs().Update(config); err != nil {
		t.Fatal("unable to update image registry config")
	}

	// give some room for the operator to act.
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	if config, err = client.Configs().Get(
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
