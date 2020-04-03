package framework

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func startOperator(te TestEnv) {
	if _, err := te.Client().Deployments(OperatorDeploymentNamespace).Patch(
		context.Background(),
		OperatorDeploymentName,
		types.MergePatchType, []byte(`{"spec": {"replicas": 1}}`),
		metav1.PatchOptions{},
	); err != nil {
		te.Fatalf("unable to start the operator: %s", err)
	}

	WaitUntilDeploymentIsRolledOut(te, OperatorDeploymentNamespace, OperatorDeploymentName)
}

func DumpOperatorDeployment(te TestEnv) {
	deployment, err := te.Client().Deployments(OperatorDeploymentNamespace).Get(
		context.Background(), OperatorDeploymentName, metav1.GetOptions{},
	)
	if err != nil {
		te.Logf("failed to get the operator deployment %v", err)
	}
	DumpYAML(te, "the operator deployment", deployment)
}

func StopDeployment(te TestEnv, namespace, name string) {
	var realErr error
	err := wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		if _, realErr = te.Client().Deployments(namespace).Patch(
			context.Background(),
			name,
			types.MergePatchType,
			[]byte(`{"spec": {"replicas": 0}}`),
			metav1.PatchOptions{},
		); realErr != nil {
			te.Logf("failed to patch delpoyment %s/%s to zero replicas: %v", namespace, name, realErr)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		te.Fatalf("unable to patch deployment %s/%s to zero replicas: %v (last error: %v)", namespace, name, err, realErr)
	}

	WaitUntilDeploymentIsRolledOut(te, namespace, name)
}

func GetOperatorLogs(client *Clientset) (PodSetLogs, error) {
	return GetLogsByLabelSelector(client, OperatorDeploymentNamespace, &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"name": "cluster-image-registry-operator",
		},
	})
}

func DumpOperatorLogs(te TestEnv) {
	podLogs, err := GetOperatorLogs(te.Client())
	if err != nil {
		te.Logf("failed to get the operator logs: %s", err)
		return
	}
	DumpPodLogs(te, podLogs)
}
