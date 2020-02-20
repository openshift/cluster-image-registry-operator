package framework

import (
	"testing"
	"time"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func MustEnsureNodeCADaemonSetIsAvailable(t *testing.T, client *Clientset) {
	err := ensureNodeCADaemonSetIsAvailable(client)
	if err != nil {
		t.Fatal(err)
	}
}

func ensureNodeCADaemonSetIsAvailable(client *Clientset) error {
	_, err := WaitForNodeCADaemonSet(client)
	if err != nil {
		return err
	}
	return nil
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
