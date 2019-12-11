package defaults

const (
	// RouteName is the name of the default route created for the registry
	// when a default route is requested from the operator
	RouteName = "default-route"

	// ImageRegistryName is the name of the image-registry workload resource (deployment)
	ImageRegistryName = "image-registry"

	// ImageRegistryResourceName is the name of the image registry config instance
	ImageRegistryResourceName = "cluster"

	// ClusterProxyResourceName is the name of the cluster proxy config instance
	ClusterProxyResourceName = "cluster"

	// CloudCredentialsName is the name of the cloud credentials secret
	CloudCredentialsName = "installer-cloud-credentials"

	// ImageRegistryCertificatesName is the name of the configmap that is managed by the
	// registry operator and mounted into the registry pod, to provide additional
	// CAs to be trusted during image pullthrough
	ImageRegistryCertificatesName = "image-registry-certificates"

	// ImageRegistryPrivateConfiguration is the name of a secret that is managed by the
	// registry operator and which provides credentials to the registry for things like
	// accessing S3 storage
	ImageRegistryPrivateConfiguration = "image-registry-private-configuration"

	// ImageRegistryPrivateConfigurationUser is the name of a secret that is managed by
	// the administrator and which provides credentials to the registry for things like
	// accessing S3 storage.  This content takes precedence over content the operator
	// automatically pulls from other locations, and it is merged into ImageRegistryPrivateConfiguration
	ImageRegistryPrivateConfigurationUser = "image-registry-private-configuration-user"

	// ImageRegistryOperatorNamespace is the namespace containing the registry operator
	// and the registry itself
	ImageRegistryOperatorNamespace = "openshift-image-registry"

	// ImageRegistryClusterOperatorResourceName is the name of the clusteroperator resource
	// that reflects the registry operator status.
	ImageRegistryClusterOperatorResourceName = "image-registry"

	// Status Conditions

	// OperatorStatusTypeRemoved denotes that the image-registry instance has been
	// removed
	OperatorStatusTypeRemoved = "Removed"

	// StorageExists denotes whether or not the registry storage medium exists
	StorageExists = "StorageExists"

	// StorageTagged denotes whether or not the registry storage medium
	// that we created was tagged correctly
	StorageTagged = "StorageTagged"

	// StorageLabeled denotes whether or not the registry storage medium
	// that we created was labeled correctly
	StorageLabeled = "StorageLabeled"

	// StorageEncrypted denotes whether or not the registry storage medium
	// that we created has encryption enabled
	StorageEncrypted = "StorageEncrypted"

	// StoragePublicAccessBlocked denotes whether or not the registry storage medium
	// that we created has had public access to itself and its objects blocked
	StoragePublicAccessBlocked = "StoragePublicAccessBlocked"

	// StorageIncompleteUploadCleanupEnabled denotes whether or not the registry storage
	// medium is configured to automatically cleanup incomplete uploads
	StorageIncompleteUploadCleanupEnabled = "StorageIncompleteUploadCleanupEnabled"

	// VersionAnnotation reflects the version of the registry that this deployment
	// is running.
	VersionAnnotation = "release.openshift.io/version"
)
