package framework

import (
	"context"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func startOperator(te TestEnv) {
	te.Logf("starting the operator...")
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

func CheckAbsenceOfOperatorPods(te TestEnv) {
	pods, err := te.Client().Pods(OperatorDeploymentNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		te.Fatalf("failed to list the pods: %s", err)
	}
	i := 0
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, OperatorDeploymentName+"-") {
			te.Errorf("unexpected operator pod %s (%s old)", pod.Name, time.Since(pod.CreationTimestamp.Time))
			i++
		}
	}
	if i > 0 {
		te.Fatalf("found %d unexpected operator pod(s)", i)
	}
}

func StopDeployment(te TestEnv, namespace, name string) {
	te.Logf("scaling down the deployment %s/%s...", namespace, name)
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
	}, false)
}

func DumpOperatorLogs(te TestEnv) {
	podLogs, err := GetOperatorLogs(te.Client())
	if err != nil {
		te.Logf("failed to get the operator logs: %s", err)
		return
	}
	DumpPodLogs(te, podLogs)
}
