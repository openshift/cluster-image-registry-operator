package framework

import (
	"github.com/openshift/cluster-image-registry-operator/defaults"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func DumpImagePrunerResource(logger Logger, client *Clientset) {
	cr, err := client.ImagePruners().Get(defaults.ImageRegistryImagePrunerResourceName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("unable to dump the image registry resource: %s", err)
		return
	}
	DumpYAML(logger, "the image pruner resource", cr)
}
