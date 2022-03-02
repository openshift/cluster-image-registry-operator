package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	gstorage "cloud.google.com/go/storage"
	"github.com/google/uuid"
	goauth2 "golang.org/x/oauth2/google"
	goption "google.golang.org/api/option"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/gcs"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	"github.com/openshift/cluster-image-registry-operator/test/framework/mock/listers"
)

func TestGCSDay2(t *testing.T) {
	ctx := context.Background()

	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	if err != nil {
		t.Fatalf("unable to create mock lister: %v", err)
	}

	mockLister, err := newMockLister.GetListers()
	if err != nil {
		t.Fatalf("unable to get listers from mock lister: %v", err)
	}

	infra, err := util.GetInfrastructure(&mockLister.StorageListers)
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.GCPPlatformType {
		t.Skip("skipping on non-GCP platform")
	}

	gcscfg, err := gcs.GetConfig(&mockLister.StorageListers)
	if err != nil {
		t.Fatalf("error reading gcs config: %v", err)
	}

	authConfig := make(map[string]string)
	if err := json.Unmarshal([]byte(gcscfg.KeyfileData), &authConfig); err != nil {
		t.Fatal("unable to unmarshal gcp auth config")
	}

	// by adding some salt in here we can check later on if the information
	// got propagated from ImageRegistryPrivateConfigurationUser and into
	// ImageRegistryPrivateConfiguration.
	authConfig["tainted"] = "some extra salt"

	taintedAuth, err := json.Marshal(authConfig)
	if err != nil {
		t.Fatal("unable to marshal gcp auth config")
	}

	// create a GCS bucket manually here so we can configure later on the
	// registry to use it.
	credentials, err := goauth2.CredentialsFromJSON(ctx, []byte(gcscfg.KeyfileData), gstorage.ScopeFullControl)
	if err != nil {
		t.Fatalf("error creating gcs credentials: %v", err)
	}

	gcli, err := gstorage.NewClient(ctx, goption.WithCredentials(credentials))
	if err != nil {
		t.Fatalf("error creating gcs client: %v", err)
	}

	randomString := strings.ReplaceAll(uuid.New().String(), "-", "")
	bucketName := fmt.Sprintf("%s-test%s", infra.Status.InfrastructureName, randomString)
	if len(bucketName) > 63 {
		bucketName = bucketName[0:63]
	}
	if err := gcli.Bucket(bucketName).Create(ctx, gcscfg.ProjectID, &gstorage.BucketAttrs{Location: gcscfg.Region}); err != nil {
		t.Fatalf("error creating bucket: %v", err)
	}
	defer func() {
		if err := gcli.Bucket(bucketName).Delete(ctx); err != nil {
			t.Errorf("error deleting bucket: %v", err)
		}
	}()

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	// Create the image-registry-private-configuration-user secret containing
	// our tainted credentials.
	err = wait.PollImmediate(time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		if _, err := framework.CreateOrUpdateSecret(
			defaults.ImageRegistryPrivateConfigurationUser,
			defaults.ImageRegistryOperatorNamespace,
			map[string]string{
				"REGISTRY_STORAGE_GCS_KEYFILE": string(taintedAuth),
			},
		); err != nil {
			t.Logf("unable to create secret: %s", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Delete(
			ctx, defaults.ImageRegistryPrivateConfigurationUser, metav1.DeleteOptions{},
		)
		if err != nil {
			t.Fatalf("error removing user secret: %v", err)
		}
	}()

	framework.DeployImageRegistry(te, &imageregistryv1.ImageRegistrySpec{
		ManagementState: operatorapi.Managed,
		Storage: imageregistryv1.ImageRegistryConfigStorage{
			GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
				Bucket: bucketName,
			},
		},
		Replicas: 1,
	})
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsSet(te)
	framework.EnsureOperatorIsNotHotLooping(te)

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information synced from the image-registry-private-configuration-user
	// secret
	imageRegistryPrivateConfiguration, err := te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Get(
		ctx, defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}
	keyfileData := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_GCS_KEYFILE"]
	if !bytes.Equal(keyfileData, taintedAuth) {
		t.Errorf("secret %s/%s contains incorrect gcs credentials", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration)
	}

	registryDeployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		ctx, defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Check that the GCS configuration environment variables
	// exist in the image registry deployment and
	// contain the correct values
	gcsEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_STORAGE", Value: "gcs", ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_GCS_BUCKET", Value: bucketName, ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_GCS_KEYFILE", Value: "/gcs/keyfile", ValueFrom: nil},
	}

	framework.CheckEnvVars(te, gcsEnvVars, registryDeployment.Spec.Template.Spec.Containers[0].Env, false)

	// Get a fresh version of the image registry resource
	_, err = te.Client().Configs().Get(
		ctx, defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("%s", err)
	}
}
