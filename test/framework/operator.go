package framework

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func startOperator(client *Clientset) error {
	if _, err := client.Deployments(OperatorDeploymentNamespace).Patch(OperatorDeploymentName, types.MergePatchType, []byte(`{"spec": {"replicas": 1}}`)); err != nil {
		return err
	}
	return nil
}

func DumpOperatorDeployment(logger Logger, client *Clientset) {
	deployment, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		logger.Logf("failed to get the operator deployment %v", err)
	}
	DumpYAML(logger, "the operator deployment", deployment)
}

func StopDeployment(logger Logger, client *Clientset, operatorDeploymentName, operatorDeploymentNamespace string) error {
	var err error
	var realErr error
	err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
		if _, realErr = client.Deployments(operatorDeploymentNamespace).Patch(operatorDeploymentName, types.MergePatchType, []byte(`{"spec": {"replicas": 0}}`)); realErr != nil {
			logger.Logf("failed to patch operator to zero replicas: %v", realErr)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return fmt.Errorf("unable to patch operator to zero replicas: %v", err)
	}

	return wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deploy, err := client.Deployments(operatorDeploymentNamespace).Get(operatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return deploy.Status.Replicas == 0, nil
	})
}

func GetOperatorLogs(client *Clientset) (PodSetLogs, error) {
	return GetLogsByLabelSelector(client, OperatorDeploymentNamespace, &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"name": "cluster-image-registry-operator",
		},
	})
}

func DumpOperatorLogs(logger Logger, client *Clientset) {
	podLogs, err := GetOperatorLogs(client)
	if err != nil {
		logger.Logf("failed to get the operator logs: %s", err)
	}
	DumpPodLogs(logger, podLogs)
}
