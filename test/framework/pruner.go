package framework

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func DumpImagePrunerResource(logger Logger, client *Clientset) {
	cr, err := client.ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry resource: %s", err)
		return
	}
	DumpYAML(logger, "the image pruner resource", cr)
}
