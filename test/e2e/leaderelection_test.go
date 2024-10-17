package e2e_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestLeaderElection(t *testing.T) {
	te := framework.Setup(t)
	defer framework.TeardownImageRegistry(te)

	numberOfReplicas := int32(3)
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		deploy, err := te.Client().Deployments(framework.OperatorDeploymentNamespace).Get(
			context.Background(), framework.OperatorDeploymentName, metav1.GetOptions{},
		)
		if err != nil {
			return err
		}

		deploy.Spec.Replicas = &numberOfReplicas

		_, err = te.Client().Deployments(framework.OperatorDeploymentNamespace).Update(
			context.Background(), deploy, metav1.UpdateOptions{},
		)
		return err
	}); err != nil {
		t.Fatalf("error updating number of operator replicas: %v", err)
	}

	framework.WaitUntilDeploymentIsRolledOut(
		te,
		framework.OperatorDeploymentNamespace,
		framework.OperatorDeploymentName,
	)

	// With the convention of leader election we need to wait a couple of seconds
	// for the pods to write the logs, so we don't get false positives
	time.Sleep(time.Second * 2)

	allLogs, err := framework.GetOperatorLogs(context.Background(), te.Client())
	if err != nil {
		t.Fatalf("error reading operator logs: %v", err)
	}

	re := regexp.MustCompile(".*successfully acquired lease.*")
	acquiredLease := 0
	for _, podLogs := range allLogs {
		for _, containerLogs := range podLogs {
			if containerLogs.Contains(re) {
				acquiredLease++
			}
		}
	}

	if acquiredLease > 1 {
		t.Errorf("multiple operators running at the same time, %d pods acquired lease, expected 1.", acquiredLease)
	}
}
