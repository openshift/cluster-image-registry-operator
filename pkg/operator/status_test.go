package operator

import (
	"fmt"
	"testing"
	"time"

	appsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
)

func validateCondition(t *testing.T, expcond, cond operatorv1.OperatorCondition) {
	if cond.Status != expcond.Status {
		t.Errorf(
			"%q condition status should be %q, %q instead",
			expcond.Type,
			expcond.Status,
			cond.Status,
		)
	}

	if cond.Reason != expcond.Reason {
		t.Errorf(
			"%q condition reason should be %q, %q instead",
			expcond.Type,
			expcond.Reason,
			cond.Reason,
		)
	}

	if cond.Message != expcond.Message {
		t.Errorf(
			"%q condition message should be %q, %q instead",
			expcond.Type,
			expcond.Message,
			cond.Message,
		)
	}
}

func Test_syncStatus(t *testing.T) {
	deployDeleteTimestamp := metav1.Now()

	for _, tt := range []struct {
		name               string
		cfg                *imageregistryv1.Config
		deploy             *appsapi.Deployment
		applyError         error
		expectedConditions []operatorv1.OperatorCondition
		routes             []*routev1.Route
	}{
		{
			name: "set as Removed but still with Deployment in place",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Removed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 8,
				},
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					Replicas:           3,
					UpdatedReplicas:    3,
					AvailableReplicas:  3,
					ObservedGeneration: 8,
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "Ready",
					Message: "The registry is ready",
				},
				{
					Type:    "Progressing",
					Status:  "True",
					Reason:  "DeletingDeployment",
					Message: "The deployment is being removed",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "Removed",
					Message: "The registry is removed",
				},
				{
					Type:    "Removed",
					Status:  "True",
					Reason:  "Removed",
					Message: "The registry is removed",
				},
			},
		},
		{
			name: "everything online and working as expected",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 8,
				},
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					Replicas:           3,
					UpdatedReplicas:    3,
					AvailableReplicas:  3,
					ObservedGeneration: 8,
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "Ready",
					Message: "The registry is ready",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "Ready",
					Message: "The registry is ready",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
			routes: []*routev1.Route{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-route",
					},
					Status: routev1.RouteStatus{
						Ingress: []routev1.RouteIngress{
							{
								RouterName: "default",
								Host:       "registry-host.openshift",
								Conditions: []routev1.RouteIngressCondition{
									{
										Type:   routev1.RouteAdmitted,
										Status: corev1.ConditionTrue,
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "another-route",
					},
					Status: routev1.RouteStatus{
						Ingress: []routev1.RouteIngress{
							{
								RouterName: "another-route",
								Host:       "another-registry-host.openshift",
								Conditions: []routev1.RouteIngressCondition{
									{
										Type:   routev1.RouteAdmitted,
										Status: corev1.ConditionTrue,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Deployment lagging some replicas",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					AvailableReplicas: 2,
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "MinimumAvailability",
					Message: "The registry has minimum availability",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "MinimumAvailability",
					Message: "The deployment has minimum availability",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment lagging some replicas (progressing with random reason)",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					AvailableReplicas: 2,
					Conditions: []appsapi.DeploymentCondition{
						{
							Type:    appsapi.DeploymentProgressing,
							Reason:  "RandomReason",
							Message: "Random message",
						},
					},
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "MinimumAvailability",
					Message: "The registry has minimum availability",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "MinimumAvailability",
					Message: "The deployment has minimum availability",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment lagging some replicas (progressing with ProgressDeadlineExceeded reason)",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					AvailableReplicas: 2,
					Conditions: []appsapi.DeploymentCondition{
						{
							Type:    appsapi.DeploymentAvailable,
							Reason:  "DoesntMatter",
							Message: "No message",
						},
						{
							Type:    appsapi.DeploymentProgressing,
							Reason:  "ProgressDeadlineExceeded",
							Message: "tired",
						},
					},
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "MinimumAvailability",
					Message: "The registry has minimum availability",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "MinimumAvailability",
					Message: "The deployment has minimum availability",
				},
				{
					Type:    "Degraded",
					Status:  "True",
					Reason:  "ProgressDeadlineExceeded",
					Message: "Registry deployment has timed out progressing: tired",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment without replicas for more than one minute",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
				Status: imageregistryv1.ImageRegistryStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type:               "Available",
								Status:             "False",
								LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							},
						},
					},
				},
			},
			deploy: &appsapi.Deployment{},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "NoReplicasAvailable",
					Message: "The deployment does not have available replicas",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "MinimumAvailability",
					Message: "The deployment has minimum availability",
				},
				{
					Type:    "Degraded",
					Status:  "True",
					Reason:  "Unavailable",
					Message: "The deployment does not have available replicas",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment without available replicas",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "NoReplicasAvailable",
					Message: "The deployment does not have available replicas",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "MinimumAvailability",
					Message: "The deployment has minimum availability",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment flagged to be deleted",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &deployDeleteTimestamp,
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "DeploymentDeleted",
					Message: "The deployment is being deleted",
				},
				{
					Type:    "Progressing",
					Status:  "True",
					Reason:  "FinalizingDeployment",
					Message: "The deployment is being deleted",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "set as Removed without Deployment in place",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Removed",
					},
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "Removed",
					Message: "The registry is removed",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "Removed",
					Message: "All registry resources are removed",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "Removed",
					Message: "The registry is removed",
				},
				{
					Type:    "Removed",
					Status:  "True",
					Reason:  "Removed",
					Message: "The registry is removed",
				},
			},
		},
		{
			name: "permanent error without Deployment in place",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			applyError: newPermanentError("Permanent", fmt.Errorf("segfault")),
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "Permanent",
					Message: "Error: segfault",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "Error",
					Message: "Unable to apply resources: segfault",
				},
				{
					Type:    "Degraded",
					Status:  "True",
					Reason:  "Permanent",
					Message: "Error: segfault",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "Deployment not in place after one minute",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
				Status: imageregistryv1.ImageRegistryStatus{
					OperatorStatus: operatorv1.OperatorStatus{
						Conditions: []operatorv1.OperatorCondition{
							{
								Type:               "Available",
								Status:             "False",
								LastTransitionTime: metav1.NewTime(time.Now().Add(-1 * time.Minute)),
							},
						},
					},
				},
			},
			applyError: fmt.Errorf("error creating deployment"),
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "DeploymentNotFound",
					Message: "The deployment does not exist",
				},
				{
					Type:    "Progressing",
					Status:  "True",
					Reason:  "Error",
					Message: "Unable to apply resources: error creating deployment",
				},
				{
					Type:    "Degraded",
					Status:  "True",
					Reason:  "Unavailable",
					Message: "The deployment does not exist",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "generic error without Deployment in place",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			applyError: fmt.Errorf("error creating deployment"),
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "DeploymentNotFound",
					Message: "The deployment does not exist",
				},
				{
					Type:    "Progressing",
					Status:  "True",
					Reason:  "Error",
					Message: "Unable to apply resources: error creating deployment",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name:   "set as Unmanaged",
			deploy: &appsapi.Deployment{},
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Unmanaged",
					},
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "Unmanaged",
					Message: "The registry configuration is set to unmanaged mode",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "Unmanaged",
					Message: "The registry configuration is set to unmanaged mode",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "Unmanaged",
					Message: "The registry configuration is set to unmanaged mode",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "set as Managed without Deployment in place",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "False",
					Reason:  "DeploymentNotFound",
					Message: "The deployment does not exist",
				},
				{
					Type:    "Progressing",
					Status:  "True",
					Reason:  "WaitingForDeployment",
					Message: "All resources are successfully applied, but the deployment does not exist",
				},
				{
					Type:    "Degraded",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
		},
		{
			name: "a faulty route",
			cfg: &imageregistryv1.Config{
				Spec: imageregistryv1.ImageRegistrySpec{
					OperatorSpec: operatorv1.OperatorSpec{
						ManagementState: "Managed",
					},
				},
			},
			deploy: &appsapi.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Generation: 8,
				},
				Spec: appsapi.DeploymentSpec{
					Replicas: ptr.To[int32](3),
				},
				Status: appsapi.DeploymentStatus{
					Replicas:           3,
					UpdatedReplicas:    3,
					AvailableReplicas:  3,
					ObservedGeneration: 8,
				},
			},
			expectedConditions: []operatorv1.OperatorCondition{
				{
					Type:    "Available",
					Status:  "True",
					Reason:  "Ready",
					Message: "The registry is ready",
				},
				{
					Type:    "Progressing",
					Status:  "False",
					Reason:  "Ready",
					Message: "The registry is ready",
				},
				{
					Type:    "Degraded",
					Status:  "True",
					Reason:  "RouteDegraded",
					Message: "route my-route (host registry-host.openshift, router default) not admitted: not working",
				},
				{
					Type:    "Removed",
					Status:  "False",
					Reason:  "",
					Message: "",
				},
			},
			routes: []*routev1.Route{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-route",
					},
					Status: routev1.RouteStatus{
						Ingress: []routev1.RouteIngress{
							{
								RouterName: "default",
								Host:       "registry-host.openshift",
								Conditions: []routev1.RouteIngressCondition{
									{
										Type:    routev1.RouteAdmitted,
										Status:  corev1.ConditionFalse,
										Message: "not working",
									},
								},
							},
						},
					},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := Controller{}
			ctrl.syncStatus(tt.cfg, tt.deploy, tt.routes, tt.applyError)
			for _, expcond := range tt.expectedConditions {
				found := false
				for _, cond := range tt.cfg.Status.Conditions {
					if cond.Type != expcond.Type {
						continue
					}
					found = true
					validateCondition(t, expcond, cond)
					break
				}
				if !found {
					t.Errorf("condition %q not found in: %+v", expcond.Type, tt.cfg.Status.Conditions)
				}
			}
		})
	}
}
