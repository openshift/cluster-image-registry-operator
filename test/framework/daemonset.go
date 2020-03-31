package framework

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func EnsureNodeCADaemonSetIsAvailable(te TestEnv) {
	_, err := WaitForNodeCADaemonSet(te.Client())
	if err != nil {
		te.Fatal(err)
	}
}

func WaitForNodeCADaemonSet(client *Clientset) (*appsv1.DaemonSet, error) {
	var ds *appsv1.DaemonSet
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		ds, err = client.DaemonSets(defaults.ImageRegistryOperatorNamespace).Get("node-ca", metav1.GetOptions{})
		if err == nil {
			if ds.Status.NumberAvailable > 0 {
				return true, nil
			}
			return false, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})
	return ds, err
}
