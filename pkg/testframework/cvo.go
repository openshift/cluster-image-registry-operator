package testframework

import (
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
)

const (
	ClusterVersionName = "version"
)

func addCompomentOverride(overrides []configv1.ComponentOverride, override configv1.ComponentOverride) ([]configv1.ComponentOverride, bool) {
	for i, o := range overrides {
		if o.Group == override.Group && o.Kind == override.Kind &&
			o.Namespace == override.Namespace && o.Name == override.Name {
			if overrides[i].Unmanaged == override.Unmanaged {
				return overrides, false
			}
			overrides[i].Unmanaged = override.Unmanaged
			return overrides, true
		}
	}
	return append(overrides, override), true
}

func DisableCVOForOperator(client *Clientset) error {
	cv, err := client.ClusterVersions().Get(ClusterVersionName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// The cluster is not managed by the Cluster Version Operator?
		return nil
	} else if err != nil {
		return err
	}

	var changed bool
	cv.Spec.Overrides, changed = addCompomentOverride(cv.Spec.Overrides, configv1.ComponentOverride{
		Group:     "", // XXX(dmage): it will be changed soon.
		Kind:      "Deployment",
		Namespace: OperatorDeploymentNamespace,
		Name:      OperatorDeploymentName,
		Unmanaged: true,
	})
	if !changed {
		return nil
	}
	if _, err := client.ClusterVersions().Update(cv); err != nil {
		return err
	}
	return nil
}
