package parameters

const (
	ChecksumOperatorAnnotation    = "dockerregistry.operator.openshift.io/checksum"
	StorageTypeOperatorAnnotation = "dockerregistry.operator.openshift.io/storagetype"

	SecretChecksumOperatorAnnotation    = "dockerregistry.operator.openshift.io/secret-checksum"
	ConfigMapChecksumOperatorAnnotation = "dockerregistry.operator.openshift.io/configmap-checksum"

	SupplementalGroupsAnnotation = "openshift.io/sa.scc.supplemental-groups"
)

type Globals struct {
	Deployment struct {
		Name      string
		Namespace string
		Labels    map[string]string
	}
	Pod struct {
		ServiceAccount string
	}
	Container struct {
		UseTLS bool
		Name   string
		Port   int
	}
	Healthz struct {
		Route          string
		TimeoutSeconds int
	}
}
