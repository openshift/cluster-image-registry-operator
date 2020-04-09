package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	configv1 "github.com/openshift/api/config/v1"
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

	err := te.Client().ConfigMaps(openshiftConfigNamespace).Delete(
		context.Background(), userCAConfigMapName, metav1.DeleteOptions{},
	)
	if err != nil && !errors.IsNotFound(err) {
		t.Fatal(err)
	}

	_, err = te.Client().ConfigMaps(openshiftConfigNamespace).Create(
		context.Background(),
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: userCAConfigMapName,
			},
			Data: caData,
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	imageConfig, err := te.Client().Images().Get(
		context.Background(), imageConfigName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("unable to get image config: %v", err)
	}

	oldAdditionalTrustedCA := imageConfig.Spec.AdditionalTrustedCA.Name
	if _, err := te.Client().Images().Patch(
		context.Background(),
		imageConfigName,
		types.MergePatchType,
		[]byte(`{"spec": {"additionalTrustedCA": {"name": "`+userCAConfigMapName+`"}}}`),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := te.Client().Images().Patch(
			context.Background(),
			imageConfigName,
			types.MergePatchType,
			[]byte(`{"spec": {"additionalTrustedCA": {"name": "`+oldAdditionalTrustedCA+`"}}}`),
			metav1.PatchOptions{},
		); err != nil {
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
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	certs, err := te.Client().ConfigMaps(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), imageRegistryCAConfigMapName, metav1.GetOptions{},
	)
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
	te := framework.SetupAvailableImageRegistry(t, nil)
	defer framework.TeardownImageRegistry(te)

	config, err := te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
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

	if _, err = te.Client().Configs().Update(
		context.Background(), config, metav1.UpdateOptions{},
	); err != nil {
		t.Fatal("unable to update image registry config")
	}

	// give some room for the operator to act.
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	if config, err = te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
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

func TestImageConfigWhenRemoved(t *testing.T) {
	hostname := "test.example.com"

	te := framework.SetupAvailableImageRegistry(t, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			EmptyDir: &imageregistryv1.ImageRegistryConfigStorageEmptyDir{},
		},
		Replicas:     1,
		DefaultRoute: true,
		Routes: []imageregistryv1.ImageRegistryConfigRoute{
			{
				Name:     "testroute",
				Hostname: hostname,
			},
		},
	})
	defer framework.TeardownImageRegistry(te)

	framework.EnsureDefaultExternalRegistryHostnameIsSet(te)
	framework.EnsureExternalRegistryHostnamesAreSet(te, []string{hostname})
	framework.EnsureInternalRegistryHostnameIsSet(te)

	if _, err := te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.JSONPatchType,
		framework.MarshalJSON([]framework.JSONPatch{
			{
				Op:    "replace",
				Path:  "/spec/managementState",
				Value: operatorapi.Removed,
			},
		}),
		metav1.PatchOptions{},
	); err != nil {
		t.Fatalf("unable to switch to removed state: %s", err)
	}

	err := wait.Poll(time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		cr, err := te.Client().Configs().Get(
			context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
		)
		if err != nil {
			return false, err
		}

		conds := framework.GetImageRegistryConditions(cr)
		t.Logf("image registry: %s", conds)
		return conds.Available.IsTrue() && conds.Available.Reason() == "Removed" &&
			conds.Progressing.IsFalse() && conds.Progressing.Reason() == "Removed" &&
			conds.Degraded.IsFalse() &&
			conds.Removed.IsTrue(), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var imgcfg *configv1.Image
	err = wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (bool, error) {
		var err error
		imgcfg, err = te.Client().Images().Get(
			context.Background(), "cluster", metav1.GetOptions{},
		)
		if errors.IsNotFound(err) {
			te.Logf("waiting for the image config resource: the resource does not exist")
			return false, nil
		} else if err != nil {
			return false, err
		}

		noExternalRoutes := len(imgcfg.Status.ExternalRegistryHostnames) == 0
		noInternalRoute := imgcfg.Status.InternalRegistryHostname == ""
		return noExternalRoutes && noInternalRoute, nil
	})
	if err != nil {
		te.Fatalf("cluster image config resource was not updated: %+v, err: %v", imgcfg, err)
	}
}
