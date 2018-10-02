package parameters

const (
	ChecksumOperatorAnnotation = "imageregistry.operator.openshift.io/checksum"

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
}
