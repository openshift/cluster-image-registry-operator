package framework

import (
	"time"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	kappsapiv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func WaitForRegistryDeployment(client *Clientset) (*kappsapiv1.Deployment, error) {
	var deployment *kappsapiv1.Deployment
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deployment, err = client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})

	return deployment, err
}

func WaitForNewRegistryDeployment(client *Clientset, currentGeneration int64) (*kappsapiv1.Deployment, error) {
	var deployment *kappsapiv1.Deployment
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deployment, err = client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(defaults.ImageRegistryName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		if deployment.Status.ObservedGeneration == currentGeneration {
			return false, nil
		}
		return false, err
	})

	return deployment, err
}

func WaitForRegistryOperatorDeployment(client *Clientset) (*kappsapiv1.Deployment, error) {
	var deployment *kappsapiv1.Deployment
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deployment, err = client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	})

	return deployment, err
}

func WaitForNewRegistryOperatorDeployment(client *Clientset, currentGeneration int64) (*kappsapiv1.Deployment, error) {
	var deployment *kappsapiv1.Deployment
	err := wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		deployment, err = client.Deployments(OperatorDeploymentNamespace).Get(OperatorDeploymentName, metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			return false, nil
		}
		if deployment.Status.ObservedGeneration == currentGeneration {
			return false, nil
		}
		return false, err
	})

	return deployment, err
}
