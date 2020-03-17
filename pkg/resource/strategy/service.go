package strategy

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func Service(o, n *corev1.Service) (bool, error) {
	dgst, err := Checksum(n)
	if err != nil {
		return false, err
	}

	if o.Annotations[defaults.ChecksumOperatorAnnotation] == dgst {
		return false, nil
	}

	Metadata(&o.ObjectMeta, &n.ObjectMeta)
	o.Spec.Selector = n.Spec.Selector
	o.Spec.Type = n.Spec.Type
	o.Spec.Ports = n.Spec.Ports

	if o.Annotations == nil {
		o.Annotations = map[string]string{}
	}
	o.Annotations[defaults.ChecksumOperatorAnnotation] = dgst

	return true, nil
}
