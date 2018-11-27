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
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/clusterconfig"
)

func TestAWS(t *testing.T) {
	cfg, err := clusterconfig.GetAWSConfig()
	if err != nil {
		t.Errorf("unable to get cluster configuration: %#v", err)
	}
	installConfig, err := clusterconfig.GetInstallConfig()
	if err != nil {
		t.Errorf("unable to get install configuration: %#v", err)
	}

	// If the storage type is not S3, skip this test.
	if cfg.Storage.Type != clusterconfig.StorageTypeS3 {
		t.Logf("Skipping S3 storage configuration tests")
		return
	}

	// Check that the image-registry-private-configuration secret exists and
	// contains the correct information
	imageRegistryPrivateConfiguration, err := getImageRegistryPrivateConfiguration()
	if err != nil {
		t.Errorf("unable to get secret %s/%s: %#v", registryNamespace, registrySecretName, err)
	}
	accessKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_ACCESSKEY"]
	secretKey, _ := imageRegistryPrivateConfiguration.Data["REGISTRY_STORAGE_S3_SECRETKEY"]
	if string(accessKey) != cfg.Storage.S3.AccessKey || string(secretKey) != cfg.Storage.S3.SecretKey {
		t.Errorf("secret %s/%s contains incorrect aws credentials (AccessKey or SecretKey)", registryNamespace, registrySecretName)
	}

	// Check that the registry operator custom resource exists
	// and contains the correct region and a non-nil bucket name
	imageRegistryOperatorCustomResource, err := getImageRegistryCustomResource()
	if err != nil {
		t.Errorf("unable to get custom resource %s/%s: %#v", registryNamespace, registryCustomResourceName, err)
	}
	if imageRegistryOperatorCustomResource.Spec.Storage.S3 == nil {
		t.Errorf("custom resource %s/%s is missing the S3 configuration", registryNamespace, registryCustomResourceName)
	} else {
		if imageRegistryOperatorCustomResource.Spec.Storage.S3.Region != cfg.Storage.S3.Region {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Region was %v but should have been %v", registryNamespace, registryCustomResourceName, cfg.Storage.S3.Region, imageRegistryOperatorCustomResource.Spec.Storage.S3)

		}
		if imageRegistryOperatorCustomResource.Spec.Storage.S3.Bucket == "" {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 Bucket name should not be empty", registryNamespace, registryCustomResourceName)
		}

		if !imageRegistryOperatorCustomResource.Status.ManagedStorage {
			t.Errorf("custom resource %s/%s contains incorrect data. S3 ManagedStorage was %v but should have been \"true\"", registryNamespace, registryCustomResourceName, imageRegistryOperatorCustomResource.Status.ManagedStorage)
		}
	}

	// Check that the S3 bucket that we created exists and is accessible
	sess, err := session.NewSession(&aws.Config{
		Credentials: credentials.NewStaticCredentials(string(accessKey), string(secretKey), ""),
		Region:      &imageRegistryOperatorCustomResource.Spec.Storage.S3.Region,
	})
	if err != nil {
		t.Errorf("unable to create new session with supplied AWS credentials")
	}
	svc := s3.New(sess)
	_, err = svc.HeadBucket(&s3.HeadBucketInput{
		Bucket: aws.String(imageRegistryOperatorCustomResource.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("s3 bucket %s does not exist or is inaccessible: %#v", imageRegistryOperatorCustomResource.Spec.Storage.S3.Bucket, err)
	}

	// Check that the S3 bucket has the correct tags
	getBucketTaggingResult, err := svc.GetBucketTagging(&s3.GetBucketTaggingInput{
		Bucket: aws.String(imageRegistryOperatorCustomResource.Spec.Storage.S3.Bucket),
	})
	if err != nil {
		t.Errorf("unable to get tagging information for s3 bucket: %#v", err)
	}

	tagShouldExist := map[string]string{
		"tectonicClusterID": installConfig.ClusterID,
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
		{Name: "REGISTRY_STORAGE_S3_BUCKET", Value: string(imageRegistryOperatorCustomResource.Spec.Storage.S3.Bucket), ValueFrom: nil},
		{Name: "REGISTRY_STORAGE_S3_REGION", Value: string(imageRegistryOperatorCustomResource.Spec.Storage.S3.Region), ValueFrom: nil},
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

	registryDeployment, err := getRegistryDeployment()

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
