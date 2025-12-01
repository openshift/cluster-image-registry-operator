package framework

import (
	"context"
	"time"

	kappsapiv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func isDeploymentRolledOut(deploy *kappsapiv1.Deployment) bool {
	replicas := int32(1)
	if deploy.Spec.Replicas != nil {
		replicas = *(deploy.Spec.Replicas)
	}
	return deploy.Status.UpdatedReplicas == replicas &&
		deploy.Status.Replicas == replicas &&
		deploy.Status.AvailableReplicas == replicas &&
		deploy.Status.ObservedGeneration >= deploy.Generation
}

func WaitUntilDeploymentIsRolledOut(ctx context.Context, te TestEnv, namespace, name string) {
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (stop bool, err error) {
			deploy, err := te.Client().Deployments(namespace).Get(
				ctx, name, metav1.GetOptions{},
			)
			if err != nil {
				return false, err
			}

			return isDeploymentRolledOut(deploy), nil
		},
	)
	if err != nil {
		te.Fatalf("failed to wait until deployment %s/%s is rolled out: %v", namespace, name, err)
	}
}

func WaitForRegistryDeployment(client *Clientset) (*kappsapiv1.Deployment, error) {
	var deployment *kappsapiv1.Deployment
	err := wait.PollUntilContextTimeout(context.Background(), 1*time.Second, AsyncOperationTimeout, false,
		func(ctx context.Context) (stop bool, err error) {
			deployment, err = client.Deployments(defaults.ImageRegistryOperatorNamespace).Get(
				ctx, defaults.ImageRegistryName, metav1.GetOptions{},
			)
			if errors.IsNotFound(err) {
				return false, nil
			} else if err != nil {
				return false, err
			}
			return true, nil
		},
	)
	return deployment, err
}
