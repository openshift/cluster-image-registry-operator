package e2e

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	imageregistryapiv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	storages3 "github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	"github.com/openshift/cluster-image-registry-operator/test/framework/mock/listers"
)

const (
	operatorServiceAccountName                = "cluster-image-registry-operator"
	defaultBoundServiceAccountTokenMountpoint = "/var/run/secrets/openshift/serviceaccount/token"
)

var (
	// Invalid AWS credentials map
	fakeAWSCredsData = map[string]string{
		"REGISTRY_STORAGE_S3_ACCESSKEY": "myAccessKey",
		"REGISTRY_STORAGE_S3_SECRETKEY": "mySecretKey",
	}
)

func TestAWSDefaults(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

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

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	// TODO: Move these checks to a conformance test run on all providers
	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
	framework.EnsureOperatorIsNotHotLooping(te)
	framework.EnsureServiceCAConfigMap(te)
	framework.EnsureNodeCADaemonSetIsAvailable(te)

	framework.CheckClusterOperatorCondition(te, "image-registry", configapiv1.OperatorUpgradeable, func(cond *configapiv1.ClusterOperatorStatusCondition, found bool) error {
		if !found {
			return fmt.Errorf("condition is not set")
		}
		if cond.Status != configapiv1.ConditionTrue {
			return fmt.Errorf("got %s, want %s", cond.Status, configapiv1.ConditionTrue)
		}
		return nil
	})

	s3Driver := storages3.NewDriver(context.Background(), nil, mockLister)
	cfg, err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Errorf("unable to get cluster configuration: %#v", err)
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information (by using it for our AWS client).
	imageRegistryPrivateConfiguration, err := te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}

	awsConfigTempFile, awsCleanupFunc, err := createAWSConfigFile(imageRegistryPrivateConfiguration, te.Client())
	if err != nil {
		t.Fatalf("failed to setup AWS client config file: %s", err)
	}
	defer awsCleanupFunc()

	// Check that the image registry resource exists
	// and contains the correct region and a non-empty bucket name
	cr, err := te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatalf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}
	if cr.Spec.Storage.S3 == nil {
		t.Fatalf("custom resource %s/%s is missing the S3 configuration", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName)
	} else {
		if cr.Spec.Storage.S3.Region != cfg.Region {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Region was %v but should have been %v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, cfg.Region, cr.Spec.Storage.S3)

		}
		if cr.Spec.Storage.S3.Bucket == "" {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Bucket name should not be empty", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName)
		}

		if cr.Spec.Storage.ManagementState != imageregistryapiv1.StorageManagementStateManaged {
			t.Errorf("custom resource %s/%s contains incorrect data. Spec.Storage.ManagementState was %v but should have been 'Managed'", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, cr.Spec.Storage.ManagementState)
		}
	}

	// Wait for the image registry resource to have an updated  conditions
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful")
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful")
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionTrue, "Enable Cleanup Successful")
	framework.ConditionExistsWithStatusAndReason(te, defaults.StoragePublicAccessBlocked, operatorapi.ConditionTrue, "Public Access Block Successful")

	// Check that the S3 bucket that we created exists and is accessible
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: &cr.Spec.Storage.S3.Region,
		},
		SharedConfigState: session.SharedConfigEnable,
		SharedConfigFiles: []string{awsConfigTempFile},
	})
	if err != nil {
		t.Errorf("unable to create new session with supplied AWS credentials")
	}

	svc := s3.New(sess)

	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("s3 bucket %s does not exist or is inaccessible: %#v", cr.Spec.Storage.S3.Bucket, err)
	}

	// Check that the S3 bucket has the correct public access settings
	getPublicAccessBlockResult, err := svc.GetPublicAccessBlock(&s3.GetPublicAccessBlockInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("unable to get public access block information for s3 bucket: %#v", err)
	} else {

		gp := getPublicAccessBlockResult.PublicAccessBlockConfiguration

		if gp.BlockPublicAcls == nil || !*gp.BlockPublicAcls {
			t.Errorf("PublicAccessBlock.BlockPublicAcls should have been true, but was %#v instead", *gp.BlockPublicAcls)
		}
		if gp.BlockPublicPolicy == nil || !*gp.BlockPublicPolicy {
			t.Errorf("PublicAccessBlock.BlockPublicPolicy should have been true, but was %#v instead", *gp.BlockPublicPolicy)
		}
		if gp.IgnorePublicAcls == nil || !*gp.IgnorePublicAcls {
			t.Errorf("PublicAccessBlock.IgnorePublicAcls should have been true, but was %#v instead", *gp.IgnorePublicAcls)
		}
		if gp.RestrictPublicBuckets == nil || !*gp.RestrictPublicBuckets {
			t.Errorf("PublicAccessBlock.RestrictPublicBuckets should have been true, but was %#v instead", *gp.RestrictPublicBuckets)
		}
	}

	// Check that the S3 bucket has the correct tags
	getBucketTaggingResult, err := svc.GetBucketTagging(&s3.GetBucketTaggingInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("unable to get tagging information for s3 bucket: %#v", err)
	}

	tagShouldExist := map[string]string{
		"kubernetes.io/cluster/" + infra.Status.InfrastructureName: "owned",
	}

	for tk, tv := range tagShouldExist {
		found := false

		for _, v := range getBucketTaggingResult.TagSet {
			if *v.Key == tk {
				found = true
				if *v.Value != tv {
					t.Errorf("s3 bucket has the wrong value for tag \"%s\": wanted %s, got %s", tk, *v.Value, tv)
				}
				break
			}
		}
		if !found {
			t.Errorf("s3 bucket does not have the tag \"%s\": got %#v", tk, getBucketTaggingResult.TagSet)
		}
	}

	// Check that the S3 bucket has the correct encryption configuration
	getBucketEncryptionResult, err := svc.GetBucketEncryption(&s3.GetBucketEncryptionInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			t.Errorf("unable to get encryption information for S3 bucket: %#v, %#v", aerr.Code(), aerr.Error())
		} else {
			t.Errorf("unknown error occurred getting encryption information for S3 bucket: %#v", err)
		}
	} else {

		wantedBucketEncryption := &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
						SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
					},
				},
			},
		}

		for _, wantedEncryptionRule := range wantedBucketEncryption.Rules {
			found := false
			for _, gotRule := range getBucketEncryptionResult.ServerSideEncryptionConfiguration.Rules {
				if reflect.DeepEqual(wantedEncryptionRule, gotRule) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("s3 encryption rule was either not found or was not correct: wanted \"%#v\": looked in %#v", wantedEncryptionRule, getBucketEncryptionResult)
			}
		}
	}

	// Check that the S3 bucket has the correct lifecycle configuration
	getBucketLifecycleConfigurationResult, err := svc.GetBucketLifecycleConfiguration(&s3.GetBucketLifecycleConfigurationInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			t.Errorf("unable to get lifecycle information for S3 bucket: %#v, %#v", aerr.Code(), aerr.Error())
		} else {
			t.Errorf("unknown error occurred getting lifecycle information for S3 bucket: %#v", err)
		}
	}
	wantedLifecycleConfiguration := &s3.BucketLifecycleConfiguration{
		Rules: []*s3.LifecycleRule{
			{
				ID:     aws.String("cleanup-incomplete-multipart-registry-uploads"),
				Status: aws.String("Enabled"),
				Filter: &s3.LifecycleRuleFilter{
					Prefix: aws.String(""),
				},
				AbortIncompleteMultipartUpload: &s3.AbortIncompleteMultipartUpload{
					DaysAfterInitiation: aws.Int64(1),
				},
			},
		},
	}
	for _, wantedRule := range wantedLifecycleConfiguration.Rules {
		found := false
		for _, gotRule := range getBucketLifecycleConfigurationResult.Rules {
			if reflect.DeepEqual(wantedRule, gotRule) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("s3 lifecycle rule was either not found or was not correct: wanted \"%#v\": looked in %#v", wantedRule, getBucketLifecycleConfigurationResult)
		}
	}

	registryDeployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the S3 configuration environment variables
	// exist in the image registry deployment and
	// contain the correct values
	awsEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_STORAGE", Value: "s3", ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: string(cr.Spec.Storage.S3.Bucket), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_REGION", Value: string(cr.Spec.Storage.S3.Region), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: fmt.Sprintf("%v", cr.Spec.Storage.S3.Encrypt), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH", Value: "/var/run/secrets/cloud/credentials", ValueFrom: nil},
	}

	for _, val := range awsEnvVars {
		found := false
		for _, v := range registryDeployment.Spec.Template.Spec.Containers[0].Env {
			if v.Name == val.Name {
				found = true
				if !reflect.DeepEqual(v, val) {
					t.Errorf("environment variable contains incorrect data: expected %#v, got %#v", val, v)
				}
			}
		}
		if !found {
			t.Errorf("unable to find environment variable: wanted %s", val.Name)
		}
	}
}

func TestAWSUnableToCreateBucketOnStartup(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("unable to get kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kubeconfig)
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

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	// Create the image-registry-private-configuration-user secret using the invalid credentials
	if _, err := framework.CreateOrUpdateSecret(defaults.ImageRegistryPrivateConfigurationUser, defaults.ImageRegistryOperatorNamespace, fakeAWSCredsData); err != nil {
		t.Fatalf("unable to create secret %q: %#v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser), err)
	}

	framework.DeployImageRegistry(te, nil)

	// Wait for the image registry resource to have an updated StorageExists condition
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorapi.ConditionFalse, "InvalidAccessKeyId")

	// Remove the image-registry-private-configuration-user secret
	err = te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Delete(
		context.Background(), defaults.ImageRegistryPrivateConfigurationUser, metav1.DeleteOptions{},
	)
	if err != nil {
		t.Errorf("unable to remove %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser, err)
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")

	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
}

func TestAWSUpdateCredentials(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

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

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)

	// Create the image-registry-private-configuration-user secret using the invalid credentials
	err = wait.PollImmediate(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		if _, err := framework.CreateOrUpdateSecret(defaults.ImageRegistryPrivateConfigurationUser, defaults.ImageRegistryOperatorNamespace, fakeAWSCredsData); err != nil {
			t.Logf("unable to create secret: %s", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Check that the user provided credentials override the system provided ones
	s3Driver := storages3.NewDriver(context.Background(), nil, mockLister)

	sharedCredentialsFile, err := s3Driver.GetCredentialsFile()
	if err != nil {
		t.Fatalf("S3 driver failed to generate credentials file: %s", err)
	}
	defer os.Remove(sharedCredentialsFile)

	credsBytes, err := ioutil.ReadFile(sharedCredentialsFile)
	if err != nil {
		t.Fatalf("failed to read in S3 driver's AWS configuration file: %s", err)
	}
	creds := string(credsBytes)
	if !strings.Contains(creds, fakeAWSCredsData["REGISTRY_STORAGE_S3_ACCESSKEY"]) || !strings.Contains(creds, fakeAWSCredsData["REGISTRY_STORAGE_S3_SECRETKEY"]) {
		t.Errorf("S3 driver's generated AWS doesn't contain expected credentials (AccessKey or SecretKey)")
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorapi.ConditionFalse, "InvalidAccessKeyId")

	// Remove the image-registry-private-configuration-user secret
	err = te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Delete(
		context.Background(), defaults.ImageRegistryPrivateConfigurationUser, metav1.DeleteOptions{},
	)
	if err != nil {
		t.Errorf("unable to remove %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser, err)
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	framework.ConditionExistsWithStatusAndReason(te, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")

	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)
}

func TestAWSChangeS3Encryption(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("unable to get kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kubeconfig)
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

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)

	cr, err := te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information
	imageRegistryPrivateConfiguration, err := te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}

	awsConfigTempFile, awsCleanup, err := createAWSConfigFile(imageRegistryPrivateConfiguration, te.Client())
	if err != nil {
		t.Fatalf("failed to setup AWS client config file: %s", err)
	}
	defer awsCleanup()

	s3Driver := storages3.NewDriver(context.Background(), nil, mockLister)
	cfg, err := s3Driver.UpdateEffectiveConfig()
	if err != nil {
		t.Errorf("unable to get cluster configuration: %#v", err)
	}
	// Check that the S3 bucket that we created exists and is accessible
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(cfg.Region),
		},
		SharedConfigState: session.SharedConfigEnable,
		SharedConfigFiles: []string{awsConfigTempFile},
	})
	if err != nil {
		t.Fatal("unable to create new session with supplied AWS credentials")
	}

	svc := s3.New(sess)
	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("s3 bucket %s does not exist or is inaccessible: %#v", cr.Spec.Storage.S3.Bucket, err)
	}

	// Check that the S3 bucket has the correct encryption configuration
	getBucketEncryptionResult, err := svc.GetBucketEncryption(&s3.GetBucketEncryptionInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			t.Errorf("unable to get encryption information for S3 bucket: %#v, %#v", aerr.Code(), aerr.Error())
		} else {

			t.Errorf("unknown error occurred getting encryption information for S3 bucket: %#v", err)
		}
	}

	wantedBucketEncryption := &s3.ServerSideEncryptionConfiguration{
		Rules: []*s3.ServerSideEncryptionRule{
			{
				ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
					SSEAlgorithm: aws.String(s3.ServerSideEncryptionAes256),
				},
			},
		},
	}

	for _, wantedEncryptionRule := range wantedBucketEncryption.Rules {
		found := false

		for _, gotRule := range getBucketEncryptionResult.ServerSideEncryptionConfiguration.Rules {
			if reflect.DeepEqual(wantedEncryptionRule, gotRule) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("s3 encryption rule was either not found or was not correct: wanted \"%#v\": looked in %#v", wantedEncryptionRule, getBucketEncryptionResult)
		}
	}

	if _, err = te.Client().Configs().Patch(
		context.Background(),
		defaults.ImageRegistryResourceName,
		types.MergePatchType,
		[]byte(`{"spec": {"storage": {"s3": {"keyID": "testKey"}}}}`),
		metav1.PatchOptions{},
	); err != nil {
		t.Errorf("unable to patch image registry custom resource: %#v", err)
	}

	found := false
	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		// Check that the S3 bucket has the correct encryption configuration
		getBucketEncryptionResult, err = svc.GetBucketEncryption(&s3.GetBucketEncryptionInput{
			Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
		})
		if aerr, ok := err.(awserr.Error); ok {
			t.Errorf("%#v, %#v", aerr.Code(), aerr.Error())
		}
		if err != nil {
			return true, err
		}

		wantedBucketEncryption = &s3.ServerSideEncryptionConfiguration{
			Rules: []*s3.ServerSideEncryptionRule{
				{
					ApplyServerSideEncryptionByDefault: &s3.ServerSideEncryptionByDefault{
						SSEAlgorithm:   aws.String(s3.ServerSideEncryptionAwsKms),
						KMSMasterKeyID: aws.String("testKey"),
					},
				},
			},
		}

		for _, wantedEncryptionRule := range wantedBucketEncryption.Rules {
			for _, gotRule := range getBucketEncryptionResult.ServerSideEncryptionConfiguration.Rules {
				if reflect.DeepEqual(wantedEncryptionRule, gotRule) {
					found = true
					break
				} else {
					return false, nil
				}
			}

		}
		return true, nil
	})
	if err != nil {
		t.Errorf("an error occurred checking for bucket encryption: %#v", err)
	}
	if !found {
		t.Errorf("s3 encryption rule was either not found or was not correct: wanted \"%#v\": looked in %#v", wantedBucketEncryption, getBucketEncryptionResult)
	}

	registryDeployment, err := te.Client().Deployments(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryName, metav1.GetOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Check that the S3 configuration environment variables
	// exist in the image registry deployment and
	// contain the correct values
	awsEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_STORAGE", Value: "s3", ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: string(cr.Spec.Storage.S3.Bucket), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_REGION", Value: string(cr.Spec.Storage.S3.Region), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_ENCRYPT", Value: fmt.Sprintf("%v", cr.Spec.Storage.S3.Encrypt), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_KEYID", Value: "testKey", ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_CREDENTIALSCONFIGPATH", Value: "/var/run/secrets/cloud/credentials", ValueFrom: nil},
	}

	for _, val := range awsEnvVars {
		found := false
		for _, v := range registryDeployment.Spec.Template.Spec.Containers[0].Env {
			if v.Name == val.Name {
				found = true
				if !reflect.DeepEqual(v, val) {
					t.Errorf("environment variable contains incorrect data: expected %#v, got %#v", val, v)
				}
			}
		}
		if !found {
			t.Errorf("unable to find environment variable: wanted %s", val.Name)
		}
	}
}

func TestAWSFinalizerDeleteS3Bucket(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

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

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	framework.DeployImageRegistry(te, nil)
	framework.WaitUntilImageRegistryIsAvailable(te)
	framework.EnsureInternalRegistryHostnameIsSet(te)
	framework.EnsureClusterOperatorStatusIsNormal(te)

	cr, err := te.Client().Configs().Get(
		context.Background(), defaults.ImageRegistryResourceName, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}

	// Save the credentials so we can verify that the S3 bucket was deleted later
	imageRegistryPrivateConfiguration, err := te.Client().Secrets(defaults.ImageRegistryOperatorNamespace).Get(
		context.Background(), defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{},
	)
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}

	// Check that the S3 bucket gets cleaned up by the finalizer (if we manage it)
	err = te.Client().Configs().Delete(
		context.Background(), defaults.ImageRegistryResourceName, metav1.DeleteOptions{},
	)
	if err != nil {
		t.Errorf("unable to get image registry resource: %#v", err)
	}

	// Create an AWS config using the in-cluster credentials so that we can watch the S3 bucket
	awsConfigTempFile, awsCleanupFunc, err := createAWSConfigFile(imageRegistryPrivateConfiguration, te.Client())
	if err != nil {
		t.Fatalf("failed to setup AWS client config file: %s", err)
	}
	defer awsCleanupFunc()

	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region: aws.String(cr.Status.Storage.S3.Region),
		},
		SharedConfigState: session.SharedConfigEnable,
		SharedConfigFiles: []string{awsConfigTempFile},
	})
	if err != nil {
		t.Fatalf("failed to build AWS session: %s", err)
	}
	s3Client := s3.New(sess)
	exists := true
	err = wait.Poll(5*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		_, err = s3Client.HeadBucket(&s3.HeadBucketInput{
			Bucket: aws.String(cr.Status.Storage.S3.Bucket),
		})

		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case s3.ErrCodeNoSuchBucket, "Forbidden", "NotFound":
				exists = false
				return true, nil
			}
		}

		return false, err
	})
	if err != nil {
		t.Errorf("an error occurred checking for s3 bucket existence: %#v", err)
	}

	if exists {
		t.Errorf("s3 bucket should have been deleted, but it wasn't: %s", err)
	}
}

// createAWSConfigFile creates an AWS credentials config based on the contents of the Secret
// containing credentials info.
// Caller is returned an cleanup function that will clean up the temporary files created.
func createAWSConfigFile(awsSecret *corev1.Secret, kubeClient *framework.Clientset) (awsConfigFilename string, cleanupFunc func(), err error) {

	secretData, ok := awsSecret.Data["credentials"]
	if !ok {
		err = fmt.Errorf("Secret did not contain expected 'credentials' field")
		return
	}
	var awsConfigTempFile *os.File

	filesToCleanup := []string{}
	cleanupFunc = func() {
		for _, file := range filesToCleanup {
			os.Remove(file)
		}
	}

	if strings.Contains(string(secretData), "web_identity_token_file") {
		// set up an STS-style AWS client
		twoHoursAsSeconds := int64(60 * 60 * 2)

		// get and store the serviceAccount token
		var tokenRequest *authenticationv1.TokenRequest
		tokenRequest, err = kubeClient.ServiceAccounts(defaults.ImageRegistryOperatorNamespace).CreateToken(context.TODO(), operatorServiceAccountName, &authenticationv1.TokenRequest{
			ObjectMeta: metav1.ObjectMeta{},
			Spec: authenticationv1.TokenRequestSpec{
				Audiences: []string{"openshift"},
				// Cheating a bit here as if the test takes too long, the token will not be
				// auto-refreshed.
				ExpirationSeconds: &twoHoursAsSeconds,
			},
		}, metav1.CreateOptions{})

		if err != nil {
			return
		}

		var tokenTempFile *os.File
		if tokenTempFile, err = ioutil.TempFile("", "cluster-image-registry-operator-test-token"); err != nil {
			return
		}
		defer tokenTempFile.Close()
		filesToCleanup = append(filesToCleanup, tokenTempFile.Name())

		if _, err = tokenTempFile.Write([]byte(tokenRequest.Status.Token)); err != nil {
			cleanupFunc()
			return
		}

		// create AWS config file pointing to token file
		if awsConfigTempFile, err = ioutil.TempFile("", "cluster-image-registry-operator-test-awsconfig"); err != nil {
			return
		}
		defer awsConfigTempFile.Close()
		filesToCleanup = append(filesToCleanup, awsConfigTempFile.Name())

		awsConfigData := strings.ReplaceAll(string(secretData), defaultBoundServiceAccountTokenMountpoint, tokenTempFile.Name())

		if _, err = awsConfigTempFile.Write([]byte(awsConfigData)); err != nil {
			cleanupFunc()
			return
		}

		awsConfigFilename = awsConfigTempFile.Name()

	} else {
		// just use the Secret contents as-is
		if awsConfigTempFile, err = ioutil.TempFile("", "cluster-image-registry-operator-test-awsconfig"); err != nil {
			return
		}
		defer awsConfigTempFile.Close()
		filesToCleanup = append(filesToCleanup, awsConfigTempFile.Name())

		if _, err = awsConfigTempFile.Write(secretData); err != nil {
			cleanupFunc()
			return
		}

		awsConfigFilename = awsConfigTempFile.Name()

	}

	return
}
