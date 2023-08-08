package gcs

import (
	"fmt"

	"k8s.io/klog/v2"

	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"
)

const (
	// ocpDefaultLabelFmt is the format string for the default label
	// added to the OpenShift created GCP resources.
	ocpDefaultLabelFmt = "kubernetes-io-cluster-%s"
)

func getUserLabels(infraLister configlisters.InfrastructureLister) (map[string]string, error) {
	infra, err := util.GetInfrastructure(infraLister)
	if err != nil {
		klog.Errorf("getUserLabels: failed to read infrastructure/cluster resource: %w", err)
		return nil, err
	}
	// add OCP default label along with user-defined labels
	labels := map[string]string{
		fmt.Sprintf(ocpDefaultLabelFmt, infra.Status.InfrastructureName): "owned",
	}
	// get user-defined labels in Infrastructure.Status.GCP
	if infra.Status.PlatformStatus != nil &&
		infra.Status.PlatformStatus.GCP != nil &&
		infra.Status.PlatformStatus.GCP.ResourceLabels != nil {
		for _, label := range infra.Status.PlatformStatus.GCP.ResourceLabels {
			labels[label.Key] = label.Value
		}
	}
	return labels, nil
}
