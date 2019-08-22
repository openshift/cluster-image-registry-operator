package framework

import (
	"fmt"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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

// MustSetOperatorDegradedTimeout ensures that the operator's degraded timeout is set and rolled out.
func MustSetOperatorDegradedTimeout(t *testing.T, client *Clientset, timeout time.Duration) {
	err := setOperatorDegradedTimeout(client, timeout)
	if err != nil {
		t.Fatalf("Failed to set operator degraded timeout to %s: %v", timeout, err)
	}
	err = wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deploy, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		t.Logf("waiting for operator deployment to be updated. Current updated replica count=%d", deploy.Status.UpdatedReplicas)
		return deploy.Status.UpdatedReplicas >= 1, nil
	})
	if err != nil {
		t.Fatalf("Failed to set operator degraded timeout to %s: %v", timeout, err)
	}
}

func setOperatorDegradedTimeout(client *Clientset, timeout time.Duration) error {
	operator, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	opContainer := operator.Spec.Template.Spec.Containers[0]
	opContainer.Env = append(opContainer.Env, corev1.EnvVar{
		Name:  "OPERATOR_DEGRADED_TIMEOUT",
		Value: timeout.String(),
	})
	operator.Spec.Template.Spec.Containers[0] = opContainer
	_, err = client.Deployments(OperatorDeploymentNamespace).Update(operator)
	return err
}

// MustClearOperatorDegradedTimeout ensures that the operator's degraded timeout has been cleared
// and has rolled out.
func MustClearOperatorDegradedTimeout(t *testing.T, client *Clientset) {
	err := clearOperatorDegradedTimeout(client)
	if err != nil {
		t.Fatalf("Failed to clear operator degraded timeout: %v", err)
	}
	err = wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deploy, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return deploy.Status.UpdatedReplicas >= 1, nil
	})
	if err != nil {
		t.Fatalf("Failed to clear operator degraded timeout: %v", err)
	}
}

func clearOperatorDegradedTimeout(client *Clientset) error {
	operator, err := client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	opContainer := operator.Spec.Template.Spec.Containers[0]
	newEnv := []corev1.EnvVar{}
	for _, env := range opContainer.Env {
		if env.Name != "OPERATOR_DEGRADED_TIMEOUT" {
			newEnv = append(newEnv, env)
		}
	}
	opContainer.Env = newEnv
	operator.Spec.Template.Spec.Containers[0] = opContainer
	_, err = client.Deployments(OperatorDeploymentNamespace).Update(operator)
	return err
}
