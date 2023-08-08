package framework

import (
	"context"
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
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (stop bool, err error) {
			ds, err = client.DaemonSets(defaults.ImageRegistryOperatorNamespace).Get(
				ctx, "node-ca", metav1.GetOptions{},
			)
			if err == nil {
				return ds.Status.NumberAvailable > 0, nil
			}
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		},
	)
	return ds, err
}

func DumpNodeCADaemonSet(te TestEnv) {
	ds, err := te.Client().DaemonSets(OperatorDeploymentNamespace).Get(
		context.Background(), "node-ca", metav1.GetOptions{},
	)
	if err != nil {
		te.Logf("failed to get the node-ca daemonset: %v", err)
	}
	DumpYAML(te, "the node-ca daemonset", ds)
}
