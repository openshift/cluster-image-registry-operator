// Copyright 2018 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	operatorapi "github.com/openshift/api/operator/v1alpha1"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

func TestAWS(t *testing.T) {
	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		t.Fatalf("unable to get install configuration: %v", err)
	}

	if installConfig.Platform.AWS == nil {
		t.Skip("skipping on non-AWS platform")
	}

	client := testframework.MustNewClientset(t, nil)

	defer testframework.MustRemoveImageRegistry(t, client)
	testframework.MustDeployImageRegistry(t, client, nil)
	testframework.MustEnsureImageRegistryIsAvailable(t, client)
	testframework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	testframework.MustEnsureClusterOperatorStatusIsSet(t, client)

	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		t.Errorf("unable to get cluster configuration: %#v", err)
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information
	imageRegistryPrivateConfiguration, err := client.Secrets(testframework.ImageRegistryDeploymentNamespace).Get(regopapi.ImageRegistryPrivateConfiguration, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", testframework.ImageRegistryDeploymentNamespace, regopapi.ImageRegistryPrivateConfiguration, err)
	}
	accessKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]
	secretKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_SECRETKEY"]
	if string(accessKey) != cfg.Storage.S3.AccessKey || string(secretKey) != cfg.Storage.S3.SecretKey {
		t.Errorf("secret %s/%s contains incorrect aws credentials (AccessKey or SecretKey)", testframework.ImageRegistryDeploymentNamespace, regopapi.ImageRegistryPrivateConfiguration)
	}

	// Check that the registry operator custom resource exists
	// and contains the correct region and a non-empty bucket name
	cr, err := client.ImageRegistries().Get(testframework.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", testframework.ImageRegistryDeploymentNamespace, testframework.ImageRegistryName, err)
	}
	if cr.Spec.Storage.S3 == nil {
		t.Errorf("custom resource %s/%s is missing the S3 configuration", testframework.ImageRegistryDeploymentNamespace, testframework.ImageRegistryName)
	} else {
		if cr.Spec.Storage.S3.Region != cfg.Storage.S3.Region {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Region was %v but should have been %v", testframework.ImageRegistryDeploymentNamespace, testframework.ImageRegistryName, cfg.Storage.S3.Region, cr.Spec.Storage.S3)

		}
		if cr.Spec.Storage.S3.Bucket == "" {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Bucket name should not be empty", testframework.ImageRegistryDeploymentNamespace, testframework.ImageRegistryName)
		}

		if !cr.Status.Storage.Managed {
			t.Errorf("custom resource %s/%s contains incorrect data. Status.Storage.Managed was %v but should have been \"true\"", testframework.ImageRegistryDeploymentNamespace, testframework.ImageRegistryName, cr.Status.Storage.Managed)
		}
		foundStorageExists := false
		for _, condition := range cr.Status.Conditions {
			if condition.Type == regopapi.StorageExists {
				foundStorageExists = true
				if condition.Status != operatorapi.ConditionTrue {
					t.Errorf("condition StorageExists should be \"true\" but was %v instead.", condition.Status)

				}
			}
		}
		if !foundStorageExists {
			t.Errorf("condition StorageExists was not found, but should have been. %#v", cr.Status.Conditions)
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

	// Check that the S3 bucket has the correct tags
	getBucketTaggingResult, err := svc.GetBucketTagging(&s3.GetBucketTaggingInput{
		Bucket: aws.String(cr.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("unable to get tagging information for s3 bucket: %#v", err)
	}

	tagShouldExist := map[string]string{
		"openshiftClusterID": installConfig.ClusterID,
	}
	for k, v := range installConfig.Platform.AWS.UserTags {
		tagShouldExist[k] = v
	}
	for tk, tv := range tagShouldExist {
		found := false

		for _, v := range getBucketTaggingResult.TagSet {
			if *v.Key == tk {
				found = true
				if *v.Value != tv {
					t.Errorf("s3 bucket has the wrong value for tag \"%s\": wanted %s, got %s", tk, *v.Value, tv)
				}
			}
		}
		if !found {
			t.Errorf("s3 bucket does not have the tag \"%s\": got %#v", tk, getBucketTaggingResult.TagSet)
		}
	}

	// Check that the S3 configuration environment variables
	// exist in the image registry deployment and
	// contain the correct values
	awsEnvVars := []corev1.EnvVar{
		{Name: "REGISTRY_STORAGE", Value: string(cfg.Storage.Type), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: string(cr.Spec.Storage.S3.Bucket), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_REGION", Value: string(cr.Spec.Storage.S3.Region), ValueFrom: nil},
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

	registryDeployment, err := client.Deployments(testframework.ImageRegistryDeploymentNamespace).Get(testframework.ImageRegistryName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
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

	// Check that the aws access key and secret key in the system configuration
	// can be overridden by a user defined secret
	fakeAWSCredsData := map[string]string{
		"REGISTRY_STORAGE_S3_ACCESSKEY": "myAccessKey",
		"REGISTRY_STORAGE_S3_SECRETKEY": "mySecretKey",
	}

	if _, err := util.CreateOrUpdateSecret(regopapi.ImageRegistryPrivateConfigurationUser, testframework.ImageRegistryDeploymentNamespace, fakeAWSCredsData); err != nil {
		t.Fatalf("unable to create secret %q: %#v", fmt.Sprintf("%s/%s", testframework.ImageRegistryDeploymentNamespace, regopapi.ImageRegistryPrivateConfigurationUser), err)
	}

	cfgUser, err := clusterconfig.GetAWSConfig()
	if err != nil {
		t.Errorf("unable to get aws configuration: %#v", err)
	}
	if fakeAWSCredsData["REGISTRY_STORAGE_S3_ACCESSKEY"] != cfgUser.Storage.S3.AccessKey || fakeAWSCredsData["REGISTRY_STORAGE_S3_SECRETKEY"] != cfgUser.Storage.S3.SecretKey {
		t.Errorf("expected system configuration to be overridden by the user configuration but it wasn't.")
	}

	// Ensure that a useful status condition is set on the image registry resource
	// if we can't create the s3 bucket because of invalid aws credentials
	testframework.MustRemoveImageRegistry(t, client)
	testframework.DeployImageRegistry(t, client, nil)

	err = wait.Poll(1*time.Second, testframework.AsyncOperationTimeout, func() (stop bool, err error) {
		// Get a fresh version of the image registry resource
		cr, err = client.ImageRegistries().Get(testframework.ImageRegistryName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}

		return true, err
	})
	if err != nil {
		testframework.DumpImageRegistryResource(t, client)
		testframework.DumpOperatorLogs(t, client)
		t.Fatal(err)
	}

	foundStorageExists := false
	for _, condition := range cr.Status.Conditions {
		if condition.Type == regopapi.StorageExists {
			foundStorageExists = true
			if condition.Status != operatorapi.ConditionFalse {
				t.Errorf("condition StorageExists should be \"false\" but was %v instead.", condition.Status)

			}
			if condition.Reason != "InvalidAccessKeyId" {
				t.Errorf("condition Reason should have been \"InvalidAccessKeyId\" but was %s instead.", condition.Reason)
			}
		}
	}
	if !foundStorageExists {
		t.Errorf("condition StorageExists was not found, but should have been. %#v", cr.Status.Conditions)
	}

	// Clean up the image-registry-private-configuration-user secret
	err = client.Secrets(testframework.ImageRegistryDeploymentNamespace).Delete(regopapi.ImageRegistryPrivateConfigurationUser, &metav1.DeleteOptions{})
	if err != nil {
		t.Errorf("unable to remove %s/%s: %#v", testframework.ImageRegistryDeploymentNamespace, regopapi.ImageRegistryPrivateConfigurationUser, err)
	}

}
