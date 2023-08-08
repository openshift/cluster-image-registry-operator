package framework

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	configv1 "github.com/openshift/api/config/v1"
)

func IsClusterOperatorHealthy(co *configv1.ClusterOperator) error {
	var errs []error
	for _, cond := range co.Status.Conditions {
		if cond.Type == configv1.OperatorAvailable && cond.Status != configv1.ConditionTrue {
			errs = append(errs, fmt.Errorf("%s unavailable (%s): %s", co.Name, cond.Reason, cond.Message))
		} else if cond.Type == configv1.OperatorProgressing && cond.Status != configv1.ConditionFalse {
			errs = append(errs, fmt.Errorf("%s progressing (%s): %s", co.Name, cond.Reason, cond.Message))
		} else if cond.Type == configv1.OperatorDegraded && cond.Status != configv1.ConditionFalse {
			errs = append(errs, fmt.Errorf("%s degraded (%s): %s", co.Name, cond.Reason, cond.Message))
		}
	}
	return utilerrors.NewAggregate(errs)
}

func AreClusterOperatorsHealthy(cos []configv1.ClusterOperator) error {
	var errs []error
	for _, co := range cos {
		errs = append(errs, IsClusterOperatorHealthy(&co))
	}
	return utilerrors.NewAggregate(errs)
}

func WaitUntilClusterOperatorsAreHealthy(te TestEnv, interval, timeout time.Duration) {
	ctx := context.Background()
	start := time.Now()
	var lastErr error
	err := wait.PollUntilContextTimeout(context.Background(), interval, timeout, true,
		func(context.Context) (stop bool, err error) {
			operators, err := te.Client().ClusterOperators().List(ctx, metav1.ListOptions{})
			if err != nil {
				return false, err
			}
			lastErr = AreClusterOperatorsHealthy(operators.Items)
			if lastErr == nil {
				return true, nil
			}
			te.Logf("waiting until cluster operators become healthy: %s", lastErr)
			return false, nil
		},
	)
	if wait.Interrupted(err) {
		te.Fatalf("cluster operators did not become healthy: %s", lastErr)
	} else if err != nil {
		te.Fatalf("error while waiting until cluster operators become healthy: %s", err)
	}

	d := time.Since(start)
	if d > interval {
		te.Logf("the cluster has recovered in %s", d)
	}
}
