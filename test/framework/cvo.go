package framework

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

func DisableCVOForOperator(te TestEnv) {
	cv, err := te.Client().ClusterVersions().Get(ClusterVersionName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		// The cluster is not managed by the Cluster Version Operator?
		return
	} else if err != nil {
		te.Fatal(err)
	}

	changed := false
	var componentChanged bool

	cv.Spec.Overrides, componentChanged = addCompomentOverride(cv.Spec.Overrides, configv1.ComponentOverride{
		Group:     "", // XXX(dmage): it will be changed soon.
		Kind:      "Deployment",
		Namespace: OperatorDeploymentNamespace,
		Name:      OperatorDeploymentName,
		Unmanaged: true,
	})
	changed = changed || componentChanged

	// Disable the kube and openshift apiserver operators so the kube+openshift apiservers don't get
	// restarted while we're running our tests.
	cv.Spec.Overrides, componentChanged = addCompomentOverride(cv.Spec.Overrides, configv1.ComponentOverride{
		Group:     "",
		Kind:      "Deployment",
		Namespace: "openshift-kube-apiserver-operator",
		Name:      "kube-apiserver-operator",
		Unmanaged: true,
	})
	changed = changed || componentChanged

	cv.Spec.Overrides, componentChanged = addCompomentOverride(cv.Spec.Overrides, configv1.ComponentOverride{
		Group:     "",
		Kind:      "Deployment",
		Namespace: "openshift-apiserver-operator",
		Name:      "openshift-apiserver-operator",
		Unmanaged: true,
	})
	changed = changed || componentChanged

	if changed {
		if _, err := te.Client().ClusterVersions().Update(cv); err != nil {
			te.Fatal(err)
		}
	}

	if err := StopDeployment(te, te.Client(), "kube-apiserver-operator", "openshift-kube-apiserver-operator"); err != nil {
		te.Fatalf("unable to stop kube apiserver operator: %v", err)
	}
	if err := StopDeployment(te, te.Client(), "openshift-apiserver-operator", "openshift-apiserver-operator"); err != nil {
		te.Fatalf("unable to stop openshift apiserver operator: %v", err)
	}
}
