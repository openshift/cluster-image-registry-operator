package framework

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

func CheckPodsAreNotRestarted(te TestEnv, labels labels.Selector) {
	pods, err := te.Client().Pods(OperatorDeploymentNamespace).List(
		context.Background(),
		metav1.ListOptions{
			LabelSelector: labels.String(),
		},
	)
	if err != nil {
		te.Fatalf("failed to list pods: %s", err)
	}
	for _, pod := range pods.Items {
		for _, container := range pod.Status.InitContainerStatuses {
			if container.RestartCount > 0 {
				te.Errorf("pod %s/%s: init container %s: restarted %d time(s)", pod.Namespace, pod.Name, container.Name, container.RestartCount)
			}
		}
		for _, container := range pod.Status.ContainerStatuses {
			if container.RestartCount > 0 {
				te.Errorf("pod %s/%s: container %s: restarted %d time(s)", pod.Namespace, pod.Name, container.Name, container.RestartCount)
			}
		}
	}
}
