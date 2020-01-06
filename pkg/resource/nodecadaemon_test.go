package resource

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kfake "k8s.io/client-go/kubernetes/fake"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func findToleration(list []corev1.Toleration, cond func(toleration corev1.Toleration) bool) *corev1.Toleration {
	for i, t := range list {
		if cond(t) {
			return &list[i]
		}
	}
	return nil
}

func TestNodeCADaemon(t *testing.T) {
	params := &parameters.Globals{}
	params.Deployment.Namespace = "openshift-image-registry"

	clientset := kfake.NewSimpleClientset()

	g := newGeneratorNodeCADaemonSet(nil, nil, clientset.AppsV1(), params)
	obj, err := g.Create()
	if err != nil {
		t.Fatal(err)
	}

	ds := obj.(*appsv1.DaemonSet)
	noScheduleToleration := findToleration(ds.Spec.Template.Spec.Tolerations, func(tol corev1.Toleration) bool {
		return tol.Key == "" && tol.Operator == "Exists" && tol.Value == "" && tol.Effect == "NoSchedule"
	})
	if noScheduleToleration == nil {
		t.Errorf("unable to find toleration for all NoSchedule taints, %#+v", ds.Spec.Template.Spec.Tolerations)
	}
}
