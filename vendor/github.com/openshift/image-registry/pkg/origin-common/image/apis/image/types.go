package image

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (

	// DefaultImageTag is used when an image tag is needed and the configuration does not specify a tag to use.
	DefaultImageTag = "latest"

	// ManagedByOpenShiftAnnotation indicates that an image is managed by OpenShift's registry.
	ManagedByOpenShiftAnnotation = "openshift.io/image.managed"

	// InsecureRepositoryAnnotation may be set true on an image stream to allow insecure access to pull content.
	InsecureRepositoryAnnotation = "openshift.io/image.insecureRepository"

	// DockerImageLayersOrderAnnotation describes layers order in the docker image.
	DockerImageLayersOrderAnnotation = "image.openshift.io/dockerLayersOrder"

	// DockerImageLayersOrderAscending indicates that image layers are sorted in
	// the order of their addition (from oldest to latest)
	DockerImageLayersOrderAscending = "ascending"

	// ImageManifestBlobStoredAnnotation indicates that manifest and config blobs of image are stored in on
	// storage of integrated Docker registry.
	ImageManifestBlobStoredAnnotation = "image.openshift.io/manifestBlobStored"

	// The supported type of image signature.
	ImageSignatureTypeAtomicImageV1 string = "AtomicImageV1"

	// DockerImageLayersOrderDescending indicates that layers are sorted in
	// reversed order of their addition (from newest to oldest).
	DockerImageLayersOrderDescending = "descending"

	// Limit that applies to images. Used with a max["storage"] LimitRangeItem to set
	// the maximum size of an image.
	LimitTypeImage corev1.LimitType = "openshift.io/Image"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Image is an immutable representation of a Docker image and metadata at a point in time.
type Image struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	// The string that can be used to pull this image.
	DockerImageReference string
	// Metadata about this image
	DockerImageMetadata DockerImage
	// This attribute conveys the version of docker metadata the JSON should be stored in, which if empty defaults to "1.0"
	DockerImageMetadataVersion string
	// The raw JSON of the manifest
	DockerImageManifest string
	// DockerImageLayers represents the layers in the image. May not be set if the image does not define that data.
	DockerImageLayers []ImageLayer
	// Signatures holds all signatures of the image.
	Signatures []ImageSignature
	// DockerImageSignatures provides the signatures as opaque blobs. This is a part of manifest schema v1.
	DockerImageSignatures [][]byte
	// DockerImageManifestMediaType specifies the mediaType of manifest. This is a part of manifest schema v2.
	DockerImageManifestMediaType string
	// DockerImageConfig is a JSON blob that the runtime uses to set up the container. This is a part of manifest schema v2.
	DockerImageConfig string
}

// ImageLayer represents a single layer of the image. Some images may have multiple layers. Some may have none.
type ImageLayer struct {
	// Name of the layer as defined by the underlying store.
	Name string
	// LayerSize of the layer as defined by the underlying store.
	LayerSize int64
	// MediaType of the referenced object.
	MediaType string
}

// +genclient
// +genclient:onlyVerbs=create,delete
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ImageSignature holds a signature of an image. It allows to verify image identity and possibly other claims
// as long as the signature is trusted. Based on this information it is possible to restrict runnable images
// to those matching cluster-wide policy.
// Mandatory fields should be parsed by clients doing image verification. The others are parsed from
// signature's content by the server. They serve just an informative purpose.
type ImageSignature struct {
	metav1.TypeMeta
	metav1.ObjectMeta

	// Required: Describes a type of stored blob.
	Type string
	// Required: An opaque binary string which is an image's signature.
	Content []byte
	// Conditions represent the latest available observations of a signature's current state.
	Conditions []SignatureCondition

	// Following metadata fields will be set by server if the signature content is successfully parsed and
	// the information available.

	// A human readable string representing image's identity. It could be a product name and version, or an
	// image pull spec (e.g. "registry.access.redhat.com/rhel7/rhel:7.2").
	ImageIdentity string
	// Contains claims from the signature.
	SignedClaims map[string]string
	// If specified, it is the time of signature's creation.
	Created *metav1.Time
	// If specified, it holds information about an issuer of signing certificate or key (a person or entity
	// who signed the signing certificate or key).
	IssuedBy *SignatureIssuer
	// If specified, it holds information about a subject of signing certificate or key (a person or entity
	// who signed the image).
	IssuedTo *SignatureSubject
}

// SignatureConditionType is a type of image signature condition.
type SignatureConditionType string

// SignatureCondition describes an image signature condition of particular kind at particular probe time.
type SignatureCondition struct {
	// Type of signature condition, Complete or Failed.
	Type SignatureConditionType
	// Status of the condition, one of True, False, Unknown.
	Status corev1.ConditionStatus
	// Last time the condition was checked.
	LastProbeTime metav1.Time
	// Last time the condition transit from one status to another.
	LastTransitionTime metav1.Time
	// (brief) reason for the condition's last transition.
	Reason string
	// Human readable message indicating details about last transition.
	Message string
}

// SignatureGenericEntity holds a generic information about a person or entity who is an issuer or a subject
// of signing certificate or key.
type SignatureGenericEntity struct {
	// Organization name.
	Organization string
	// Common name (e.g. openshift-signing-service).
	CommonName string
}

// SignatureIssuer holds information about an issuer of signing certificate or key.
type SignatureIssuer struct {
	SignatureGenericEntity
}

// SignatureSubject holds information about a person or entity who created the signature.
type SignatureSubject struct {
	SignatureGenericEntity
	// If present, it is a human readable key id of public key belonging to the subject used to verify image
	// signature. It should contain at least 64 lowest bits of public key's fingerprint (e.g.
	// 0x685ebe62bf278440).
	PublicKeyID string
}

// DockerImageReference points to a Docker image.
type DockerImageReference struct {
	Registry  string
	Namespace string
	Name      string
	Tag       string
	ID        string
}
