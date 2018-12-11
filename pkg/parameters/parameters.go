package parameters

const (
	ImageRegistryOperatorResourceFinalizer = "imageregistry.operator.openshift.io/finalizer"

	ChecksumOperatorAnnotation     = "imageregistry.operator.openshift.io/checksum"
	ChecksumOperatorDepsAnnotation = "imageregistry.operator.openshift.io/dependencies-checksum"

	SupplementalGroupsAnnotation = "openshift.io/sa.scc.supplemental-groups"
)

type Globals struct {
	Deployment struct {
		Namespace string
		Labels    map[string]string
	}
	Pod struct {
		ServiceAccount string
	}
	Container struct {
		Port int
	}
	Healthz struct {
		Route          string
		TimeoutSeconds int
	}
	Service struct {
		Name string
	}
	ImageConfig struct {
		Name string
	}
	CAConfig struct {
		Name string
	}
}
