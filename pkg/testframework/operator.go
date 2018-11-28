package testframework

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	OperatorDeploymentNamespace = "openshift-image-registry"
	OperatorDeploymentName      = "cluster-image-registry-operator"
)

func startOperator(client *Clientset) error {
	if _, err := client.Deployments(OperatorDeploymentNamespace).Patch(OperatorDeploymentName, types.MergePatchType, []byte(`{"spec": {"replicas": "1"}}`)); err != nil {
		return err
	}
	return nil
}

func stopOperator(client *Clientset) error {
	if _, err := client.Deployments(OperatorDeploymentNamespace).Patch(OperatorDeploymentName, types.MergePatchType, []byte(`{"spec": {"replicas": "0"}}`)); err != nil {
		return err
	}

	return wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deploy, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
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
