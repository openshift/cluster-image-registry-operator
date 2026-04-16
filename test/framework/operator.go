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

	WaitUntilDeploymentIsRolledOut(context.Background(), te, OperatorDeploymentNamespace, OperatorDeploymentName)
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

func WaitForOperatorPodsToBeDeleted(te TestEnv) {
	te.Logf("waiting for operator pods to be deleted...")
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			pods, err := te.Client().Pods(OperatorDeploymentNamespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			for _, pod := range pods.Items {
				if strings.HasPrefix(pod.Name, OperatorDeploymentName+"-") {
					te.Logf("waiting for operator pod %s to be deleted (age: %s)...", pod.Name, time.Since(pod.CreationTimestamp.Time))
					return false, nil
				}
			}
			return true, nil
		},
	)
	if err != nil {
		te.Fatalf("failed to wait for operator pods to be deleted: %v", err)
	}
}

func WaitForOperatorPodsToStabilize(te TestEnv, expectedCount int) {
	te.Logf("waiting for operator pods to stabilize at count %d...", expectedCount)
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (bool, error) {
			pods, err := te.Client().Pods(OperatorDeploymentNamespace).List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			runningCount := 0
			terminatingCount := 0
			for _, pod := range pods.Items {
				if strings.HasPrefix(pod.Name, OperatorDeploymentName+"-") {
					if pod.DeletionTimestamp != nil {
						terminatingCount++
					} else {
						runningCount++
					}
				}
			}
			if runningCount == expectedCount && terminatingCount == 0 {
				return true, nil
			}
			te.Logf("waiting for pods to stabilize: running=%d (expected=%d), terminating=%d...", runningCount, expectedCount, terminatingCount)
			return false, nil
		},
	)
	if err != nil {
		te.Fatalf("failed to wait for operator pods to stabilize: %v", err)
	}
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
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 30*time.Second, false,
		func(ctx context.Context) (bool, error) {
			if _, realErr = te.Client().Deployments(namespace).Patch(
				ctx,
				name,
				types.MergePatchType,
				[]byte(`{"spec": {"replicas": 0}}`),
				metav1.PatchOptions{},
			); realErr != nil {
				te.Logf("failed to patch deployment %s/%s to zero replicas: %v", namespace, name, realErr)
				return false, nil
			}
			return true, nil
		},
	)
	if err != nil {
		te.Fatalf("unable to patch deployment %s/%s to zero replicas: %v (last error: %v)", namespace, name, err, realErr)
	}

	WaitUntilDeploymentIsRolledOut(context.Background(), te, namespace, name)
}

func GetOperatorLogs(ctx context.Context, client *Clientset) (PodSetLogs, error) {
	return GetLogsByLabelSelector(ctx, client, OperatorDeploymentNamespace, &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"name": "cluster-image-registry-operator",
		},
	}, false)
}

func DumpOperatorLogs(ctx context.Context, te TestEnv) {
	podLogs, err := GetOperatorLogs(ctx, te.Client())
	if err != nil {
		te.Logf("failed to get the operator logs: %s", err)
		return
	}
	DumpPodLogs(te, podLogs)
}
