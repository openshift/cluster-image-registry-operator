package e2e

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	configapiv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
	storages3 "github.com/openshift/cluster-image-registry-operator/pkg/storage/s3"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
	"github.com/openshift/cluster-image-registry-operator/test/framework/mock/listers"
)

var (
	// Invalid AWS credentials map
	fakeAWSCredsData = map[string]string{
		"REGISTRY_STORAGE_S3_ACCESSKEY": "myAccessKey",
		"REGISTRY_STORAGE_S3_SECRETKEY": "mySecretKey",
	}
)

func TestAWSDefaults(t *testing.T) {
	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	mockLister, err := newMockLister.GetListers()

	infra, err := mockLister.Infrastructures.Get("cluster")
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	client := framework.MustNewClientset(t, nil)

	// TODO: Move these checks to a conformance test run on all providers
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)
	framework.MustEnsureServiceCAConfigMap(t, client)
	framework.MustEnsureNodeCADaemonSetIsAvailable(t, client)

	cfg, err := storages3.GetConfig(kcfg, mockLister)
	if err != nil {
		t.Errorf("unable to get cluster configuration: %#v", err)
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information
	imageRegistryPrivateConfiguration, err := client.Secrets(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}
	accessKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]
	secretKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_SECRETKEY"]
	if string(accessKey) != cfg.AccessKey || string(secretKey) != cfg.SecretKey {
		t.Errorf("secret %s/%s contains incorrect aws credentials (AccessKey or SecretKey)", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration)
	}

	// Check that the image registry resource exists
	// and contains the correct region and a non-empty bucket name
	cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}
	if cr.Spec.Storage.S3 == nil {
		t.Errorf("custom resource %s/%s is missing the S3 configuration", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName)
	} else {
		if cr.Spec.Storage.S3.Region != cfg.Region {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Region was %v but should have been %v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, cfg.Region, cr.Spec.Storage.S3)

		}
		if cr.Spec.Storage.S3.Bucket == "" {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Bucket name should not be empty", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName)
		}

		if !cr.Status.StorageManaged {
			t.Errorf("custom resource %s/%s contains incorrect data. Status.StorageManaged was %v but should have been \"true\"", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, cr.Status.StorageManaged)
		}
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	errs := framework.ConditionExistsWithStatusAndReason(client, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Wait for the image registry resource to have an updated StorageTagged condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StorageTagged, operatorapi.ConditionTrue, "Tagging Successful")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Wait for the image registry resource to have an updated StorageEncrypted condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StorageEncrypted, operatorapi.ConditionTrue, "Encryption Successful")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Wait for the image registry resource to have an updated StorageIncompleteUploadCleanupEnabled condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StorageIncompleteUploadCleanupEnabled, operatorapi.ConditionTrue, "Enable Cleanup Successful")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Wait for the image registry resource to have an updated StoragePublicAccessBlocked condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StoragePublicAccessBlocked, operatorapi.ConditionTrue, "Public Access Block Successful")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Check that the S3 bucket that we created exists and is accessible
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(string(accessKey), string(secretKey), ""),
		Region:      &cr.Spec.Storage.S3.Region,
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
	}

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

	registryDeployment, err := client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
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
		{Name: "REGISTRY_STORAGE_S3_ACCESSKEY", Value: "", ValueFrom: &corev1.EnvVarSource{
			FieldRef: nil, ResourceFieldRef: nil, ConfigMapKeyRef: nil, SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "image-registry-private-configuration"},
				Key: "REGISTRY_STORAGE_S3_ACCESSKEY"},
		},
		},
		{Name: "REGISTRY_STORAGE_S3_SECRETKEY", Value: "", ValueFrom: &corev1.EnvVarSource{
			FieldRef: nil, ResourceFieldRef: nil, ConfigMapKeyRef: nil, SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "image-registry-private-configuration"},
				Key: "REGISTRY_STORAGE_S3_SECRETKEY"},
		},
		},
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
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("unable to get kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kubeconfig)
	mockLister, err := newMockLister.GetListers()

	infra, err := mockLister.Infrastructures.Get("cluster")
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	client := framework.MustNewClientset(t, nil)

	// Create the image-registry-private-configuration-user secret using the invalid credentials
	if _, err := framework.CreateOrUpdateSecret(defaults.ImageRegistryPrivateConfigurationUser, defaults.ImageRegistryOperatorNamespace, fakeAWSCredsData); err != nil {
		t.Fatalf("unable to create secret %q: %#v", fmt.Sprintf("%s/%s", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser), err)
	}

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)

	// Wait for the image registry resource to have an updated StorageExists condition
	errs := framework.ConditionExistsWithStatusAndReason(client, defaults.StorageExists, operatorapi.ConditionFalse, "InvalidAccessKeyId")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Remove the image-registry-private-configuration-user secret
	err = client.Secrets(defaults.ImageRegistryOperatorNamespace).Delete(defaults.ImageRegistryPrivateConfigurationUser, &metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("unable to remove %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser, err)
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
}

func TestAWSUpdateCredentials(t *testing.T) {
	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	mockLister, err := newMockLister.GetListers()

	infra, err := mockLister.Infrastructures.Get("cluster")
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

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
	cfgUser, err := storages3.GetConfig(kcfg, mockLister)
	if err != nil {
		t.Errorf("unable to get aws configuration: %#v", err)
	}
	if fakeAWSCredsData["REGISTRY_STORAGE_S3_ACCESSKEY"] != cfgUser.AccessKey || fakeAWSCredsData["REGISTRY_STORAGE_S3_SECRETKEY"] != cfgUser.SecretKey {
		t.Errorf("expected system configuration to be overridden by the user configuration but it wasn't.")
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	errs := framework.ConditionExistsWithStatusAndReason(client, defaults.StorageExists, operatorapi.ConditionFalse, "InvalidAccessKeyId")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	// Remove the image-registry-private-configuration-user secret
	err = client.Secrets(defaults.ImageRegistryOperatorNamespace).Delete(defaults.ImageRegistryPrivateConfigurationUser, &metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("unable to remove %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfigurationUser, err)
	}

	// Wait for the image registry resource to have an updated StorageExists condition
	errs = framework.ConditionExistsWithStatusAndReason(client, defaults.StorageExists, operatorapi.ConditionTrue, "S3 Bucket Exists")
	if len(errs) != 0 {
		for _, err := range errs {
			t.Errorf("%#v", err)
		}
	}

	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
}

func TestAWSChangeS3Encryption(t *testing.T) {
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("unable to get kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kubeconfig)
	mockLister, err := newMockLister.GetListers()

	infra, err := mockLister.Infrastructures.Get("cluster")
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information
	imageRegistryPrivateConfiguration, err := client.Secrets(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryPrivateConfiguration, err)
	}
	accessKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]
	secretKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_SECRETKEY"]

	// Check that the S3 bucket that we created exists and is accessible
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(string(accessKey), string(secretKey), ""),
		Region:      &cr.Spec.Storage.S3.Region,
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

	if _, err = client.Configs().Patch(defaults.ImageRegistryResourceName, types.MergePatchType, []byte(`{"spec": {"storage": {"s3": {"keyID": "testKey"}}}}`)); err != nil {
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

	registryDeployment, err := client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
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
		{Name: "REGISTRY_STORAGE_S3_ACCESSKEY", Value: "", ValueFrom: &corev1.EnvVarSource{
			FieldRef: nil, ResourceFieldRef: nil, ConfigMapKeyRef: nil, SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "image-registry-private-configuration"},
				Key: "REGISTRY_STORAGE_S3_ACCESSKEY"},
		},
		},
		{Name: "REGISTRY_STORAGE_S3_SECRETKEY", Value: "", ValueFrom: &corev1.EnvVarSource{
			FieldRef: nil, ResourceFieldRef: nil, ConfigMapKeyRef: nil, SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "image-registry-private-configuration"},
				Key: "REGISTRY_STORAGE_S3_SECRETKEY"},
		},
		},
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
	kcfg, err := regopclient.GetConfig()
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err)
	}

	newMockLister, err := listers.NewMockLister(kcfg)
	mockLister, err := newMockLister.GetListers()

	infra, err := mockLister.Infrastructures.Get("cluster")
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.AWSPlatformType {
		t.Skip("skipping on non-AWS platform")
	}

	client := framework.MustNewClientset(t, nil)

	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	cr, err := client.Configs().Get(defaults.ImageRegistryResourceName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", defaults.ImageRegistryOperatorNamespace, defaults.ImageRegistryResourceName, err)
	}
	// Check that the S3 bucket gets cleaned up by the finalizer (if we manage it)
	err = client.Configs().Delete(defaults.ImageRegistryResourceName, &metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("unable to get image registry resource: %#v", err)
	}
	driver, err := storage.NewDriver(&cr.Spec.Storage, kcfg, mockLister)
	if err != nil {
		t.Fatal("unable to create new s3 driver")
	}

	var exists bool
	err = wait.Poll(1*time.Second, framework.AsyncOperationTimeout, func() (stop bool, err error) {
		exists, err := driver.StorageExists(cr)
		if aerr, ok := err.(awserr.Error); ok {
			t.Errorf("%#v, %#v", aerr.Code(), aerr.Error())
		}
		if err != nil {
			return true, err
		}
		if exists {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Errorf("an error occurred checking for s3 bucket existence: %#v", err)
	}

	if exists {
		t.Errorf("s3 bucket should have been deleted, but it wasn't")
	}

}
