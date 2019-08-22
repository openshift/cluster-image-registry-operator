package operator

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	imageregistryv1 "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDegradedTimeoutExceeded(t *testing.T) {
	cases := []struct {
		name     string
		config   *imageregistryv1.Config
		timeout  time.Duration
		expected bool
	}{
		{
			name: "null",
		},
		{
			name:   "empty",
			config: &imageregistryv1.Config{},
		},
		{
			name:    "does not exceed",
			timeout: 1 * time.Hour,
			config: &imageregistryv1.Config{
				Status: imageregistryv1.ImageRegistryStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type:               operatorv1.OperatorStatusTypeDegraded,
								Status:             operatorv1.ConditionTrue,
								LastTransitionTime: metav1.Now(),
							},
						},
					},
				},
			},
		},
		{
			name:    "does exceed",
			timeout: 1 * time.Second,
			config: &imageregistryv1.Config{
				Status: imageregistryv1.ImageRegistryStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type:               operatorv1.OperatorStatusTypeDegraded,
								Status:             operatorv1.ConditionTrue,
								LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							},
						},
					},
				},
			},
			expected: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			controller := &Controller{
				degradedTimeout: tc.timeout,
			}
			exceeded := controller.degradedTimeoutExceeded(tc.config, operatorv1.OperatorStatusTypeDegraded)
			if tc.expected != exceeded {
				t.Errorf("expected degraded exceeded=%t, got %t", tc.expected, exceeded)
			}
		})
	}
}
