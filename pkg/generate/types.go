package generate

import (
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/dockerregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

type Generator func(*v1alpha1.OpenShiftDockerRegistry, *parameters.Globals) (Template, error)
