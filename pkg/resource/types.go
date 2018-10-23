package resource

import (
	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

type Generator func(*v1alpha1.ImageRegistry, *parameters.Globals) (Template, error)
