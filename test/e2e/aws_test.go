package e2e

import (
	"testing"
)

func TestAWSDefaults(t *testing.T) {
	testAWSDefaults(t)
}

func TestAWSUnableToCreateBucketOnStartup(t *testing.T) {
	testAWSUnableToCreateBucketOnStartup(t)
}

func TestAWSUpdateCredentials(t *testing.T) {
	testAWSUpdateCredentials(t)
}

func TestAWSChangeS3Encryption(t *testing.T) {
	testAWSChangeS3Encryption(t)
}

func TestAWSFinalizerDeleteS3Bucket(t *testing.T) {
	testAWSFinalizerDeleteS3Bucket(t)
}
