package e2e

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	"github.com/openshift/cluster-image-registry-operator/test/framework/mock/listers"
)

var (
	// Invalid GCS credentials data
	fakeGCSKeyfile = `{
  "type": "service_account",
  "project_id": "openshift-test-project",
  "private_key_id": "aabbccddeeffgghhiijjkkllmmnnooppqqrrssttuuvvwwxxyyzz",
  "private_key": "-----BEGIN PRIVATE KEY-----\n556B58703273357638792F423F4528482B4D6251655468566D597133743677397A24432646294A404E635266556A586E5A7234753778214125442A472D4B6150645367566B59703373357638792F423F4528482B4D6251655468576D5A7134743777397A24432646294A404E635266556A586E3272357538782F4125442A472D==\n-----END PRIVATE KEY-----\n",
  "client_email": "image-registy-testing@openshift-test-project.iam.gserviceaccount.com",
  "client_id": "123456789101112131415",
  "auth_uri": "https://accounts.google.com/o/oauth2/auth",
  "token_uri": "https://oauth2.googleapis.com/token",
  "auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
  "client_x509_cert_url": "https://www.googleapis.com/robot/v1/metadata/x509/image-registy-testing%40openshift-test-project.iam.gserviceaccount.com"
}`
	fakeGCSCredsData = map[string]string{
		"REGISTRY_STORAGE_GCS_KEYFILE": fakeGCSKeyfile,
	}
)

// Refactor the GCS Tests
// TestGCSMinimal is a test to verify that GCS credentials
// provided as part of the Day 2 experience will be propagated to the
// image-registry deployment
func TestGCSMinimal(t *testing.T) {
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

	infra, err := util.GetInfrastructure(mockLister)
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.GCPPlatformType {
		t.Skip("skipping on non-GCP platform")
	}

	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	// Create the image-registry-private-configuration-user secret using the invalid credentials
	err = wait.PollImmediate(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		if _, err := framework.CreateOrUpdateSecret(defaults.ImageRegistryPrivateConfigurationUser, defaults.ImageRegistryOperatorNamespace, fakeGCSCredsData); err != nil {
			t.Logf("unable to create secret: %s", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	const bucketName = "openshift-test-bucket"

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
	// contains the correct information synced from the image-registry-private-configuration-user secret
	imageRegistryPrivateConfiguration, err := te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}
	keyfileData := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_GCS_KEYFILE"]
	if string(keyfileData) != fakeGCSKeyfile {
		t.Errorf("secret %s/%s contains incorrect gcs credentials", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration)
	}

	registryDeployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
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
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("%s", err)
	}
}
