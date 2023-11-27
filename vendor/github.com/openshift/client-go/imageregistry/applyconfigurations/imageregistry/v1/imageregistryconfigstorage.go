// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/openshift/api/imageregistry/v1"
)

// ImageRegistryConfigStorageApplyConfiguration represents an declarative configuration of the ImageRegistryConfigStorage type for use
// with apply.
type ImageRegistryConfigStorageApplyConfiguration struct {
	EmptyDir        *v1.ImageRegistryConfigStorageEmptyDir                  `json:"emptyDir,omitempty"`
	S3              *ImageRegistryConfigStorageS3ApplyConfiguration         `json:"s3,omitempty"`
	GCS             *ImageRegistryConfigStorageGCSApplyConfiguration        `json:"gcs,omitempty"`
	Swift           *ImageRegistryConfigStorageSwiftApplyConfiguration      `json:"swift,omitempty"`
	PVC             *ImageRegistryConfigStoragePVCApplyConfiguration        `json:"pvc,omitempty"`
	Azure           *ImageRegistryConfigStorageAzureApplyConfiguration      `json:"azure,omitempty"`
	IBMCOS          *ImageRegistryConfigStorageIBMCOSApplyConfiguration     `json:"ibmcos,omitempty"`
	OSS             *ImageRegistryConfigStorageAlibabaOSSApplyConfiguration `json:"oss,omitempty"`
	ManagementState *string                                                 `json:"managementState,omitempty"`
}

// ImageRegistryConfigStorageApplyConfiguration constructs an declarative configuration of the ImageRegistryConfigStorage type for use with
// apply.
func ImageRegistryConfigStorage() *ImageRegistryConfigStorageApplyConfiguration {
	return &ImageRegistryConfigStorageApplyConfiguration{}
}

// WithEmptyDir sets the EmptyDir field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the EmptyDir field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithEmptyDir(value v1.ImageRegistryConfigStorageEmptyDir) *ImageRegistryConfigStorageApplyConfiguration {
	b.EmptyDir = &value
	return b
}

// WithS3 sets the S3 field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the S3 field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithS3(value *ImageRegistryConfigStorageS3ApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.S3 = value
	return b
}

// WithGCS sets the GCS field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the GCS field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithGCS(value *ImageRegistryConfigStorageGCSApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.GCS = value
	return b
}

// WithSwift sets the Swift field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Swift field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithSwift(value *ImageRegistryConfigStorageSwiftApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.Swift = value
	return b
}

// WithPVC sets the PVC field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the PVC field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithPVC(value *ImageRegistryConfigStoragePVCApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.PVC = value
	return b
}

// WithAzure sets the Azure field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Azure field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithAzure(value *ImageRegistryConfigStorageAzureApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.Azure = value
	return b
}

// WithIBMCOS sets the IBMCOS field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the IBMCOS field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithIBMCOS(value *ImageRegistryConfigStorageIBMCOSApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.IBMCOS = value
	return b
}

// WithOSS sets the OSS field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the OSS field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithOSS(value *ImageRegistryConfigStorageAlibabaOSSApplyConfiguration) *ImageRegistryConfigStorageApplyConfiguration {
	b.OSS = value
	return b
}

// WithManagementState sets the ManagementState field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ManagementState field is set to the value of the last call.
func (b *ImageRegistryConfigStorageApplyConfiguration) WithManagementState(value string) *ImageRegistryConfigStorageApplyConfiguration {
	b.ManagementState = &value
	return b
}