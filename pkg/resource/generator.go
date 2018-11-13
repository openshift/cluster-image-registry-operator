package resource

import (
	"fmt"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

type Generator func(*regopapi.ImageRegistry, *parameters.Globals) (Template, error)

func Templates(cr *regopapi.ImageRegistry, p *parameters.Globals) (ret []Template, err error) {
	generators := []Generator{
		ClusterRole,
		ClusterRoleBinding,
		ServiceAccount,
		ConfigMap,
		Secret,
		Service,
		ImageConfig,
	}

	routes := GetRouteGenerators(cr, p)
	for i := range routes {
		generators = append(generators, routes[i])
	}

	ret = make([]Template, len(generators)+1)
	resourceData := make([]string, len(generators))

	for i, gen := range generators {
		ret[i], err = gen(cr, p)
		if err != nil {
			return
		}

		resourceData[i], err = checksum(ret[i].Expected())
		if err != nil {
			err = fmt.Errorf("unable to generate checksum for %s: %s", ret[i].Name(), err)
			return
		}
	}

	deploymentTmpl, err := Deployment(cr, p)
	if err != nil {
		return
	}

	dgst, err := checksum(resourceData)
	if err != nil {
		err = fmt.Errorf("unable to generate checksum for %s dependencies: %s", deploymentTmpl.Name(), err)
		return
	}

	if deploymentTmpl.Annotations == nil {
		deploymentTmpl.Annotations = make(map[string]string)
	}

	deploymentTmpl.Annotations[parameters.ChecksumOperatorDepsAnnotation] = dgst

	ret[len(generators)] = deploymentTmpl

	return
}
