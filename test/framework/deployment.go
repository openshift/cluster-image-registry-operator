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
	var lastDeploy *kappsapiv1.Deployment
	err := wait.PollUntilContextTimeout(ctx, 1*time.Second, AsyncOperationTimeout, false,
		func(context.Context) (stop bool, err error) {
			// Use the caller context, not the poll timeout context. Cancelling
			// the in-flight GET on poll timeout makes client-go report a rate
			// limiter error instead of the wait timeout.
			deploy, err := te.Client().Deployments(namespace).Get(
				ctx, name, metav1.GetOptions{},
			)
			if err != nil {
				te.Logf("failed to get deployment %s/%s: %v", namespace, name, err)
				return false, nil
			}
			lastDeploy = deploy
			return isDeploymentRolledOut(deploy), nil
		},
	)
	if err != nil {
		if lastDeploy != nil {
			te.Logf("deployment %s/%s status: replicas=%d updated=%d available=%d unavailable=%d observedGeneration=%d generation=%d conditions=%v",
				namespace, name,
				lastDeploy.Status.Replicas,
				lastDeploy.Status.UpdatedReplicas,
				lastDeploy.Status.AvailableReplicas,
				lastDeploy.Status.UnavailableReplicas,
				lastDeploy.Status.ObservedGeneration,
				lastDeploy.Generation,
				lastDeploy.Status.Conditions,
			)
		}
		pods, perr := te.Client().Pods(namespace).List(context.WithoutCancel(ctx), metav1.ListOptions{})
		if perr != nil {
			te.Logf("failed to list pods in %s: %v", namespace, perr)
		} else {
			for _, pod := range pods.Items {
				te.Logf("pod %s/%s phase=%s reason=%s message=%s", pod.Namespace, pod.Name, pod.Status.Phase, pod.Status.Reason, pod.Status.Message)
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.State.Waiting != nil {
						te.Logf("  container %s waiting reason=%s message=%s", cs.Name, cs.State.Waiting.Reason, cs.State.Waiting.Message)
					}
					if cs.State.Terminated != nil {
						te.Logf("  container %s terminated reason=%s message=%s", cs.Name, cs.State.Terminated.Reason, cs.State.Terminated.Message)
					}
				}
			}
		}
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
