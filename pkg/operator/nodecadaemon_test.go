package operator

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kubeinformers "k8s.io/client-go/informers"
	kubefakeclient "k8s.io/client-go/kubernetes/fake"
	clientgotesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"

	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	imageregistryfakeclient "github.com/openshift/client-go/imageregistry/clientset/versioned/fake"
	imageregistryinformers "github.com/openshift/client-go/imageregistry/informers/externalversions"
	"github.com/openshift/library-go/pkg/operator/events"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
)

func TestNodeCADaemonControllerDegradedInertia(t *testing.T) {
	applyError := fmt.Errorf("simulated API server error")
	degradedTrue := operatorv1.ConditionTrue
	degradedFalse := operatorv1.ConditionFalse
	tests := []struct {
		name                  string
		applyError            error
		syncFailureSince      time.Time
		expectSyncError       bool
		expectDegradedStatus  *operatorv1.ConditionStatus
		expectFailureSinceSet bool
	}{
		{
			name:                  "first sync error records failure time but does not set Degraded",
			applyError:            applyError,
			expectSyncError:       true,
			expectDegradedStatus:  nil,
			expectFailureSinceSet: true,
		},
		{
			name:                  "sync error within inertia window does not set Degraded",
			applyError:            applyError,
			syncFailureSince:      time.Now(),
			expectSyncError:       true,
			expectDegradedStatus:  nil,
			expectFailureSinceSet: true,
		},
		{
			name:                  "sync error past inertia window sets Degraded",
			applyError:            applyError,
			syncFailureSince:      time.Now().Add(-nodeCADaemonControllerDegradedInertia - time.Second),
			expectSyncError:       true,
			expectDegradedStatus:  &degradedTrue,
			expectFailureSinceSet: true,
		},
		{
			name:                  "successful sync resets failure timestamp",
			applyError:            nil,
			syncFailureSince:      time.Now().Add(-time.Minute),
			expectSyncError:       false,
			expectDegradedStatus:  &degradedFalse,
			expectFailureSinceSet: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller, regClient := newNodeCATestSetup(t, tt.applyError)
			controller.syncFailureSince = tt.syncFailureSince

			err := controller.sync()
			if tt.expectSyncError && err == nil {
				t.Fatal("expected sync to return an error")
			}
			if !tt.expectSyncError && err != nil {
				t.Fatalf("unexpected sync error: %v", err)
			}

			if tt.expectFailureSinceSet && controller.syncFailureSince.IsZero() {
				t.Error("expected syncFailureSince to be set")
			}
			if !tt.expectFailureSinceSet && !controller.syncFailureSince.IsZero() {
				t.Error("expected syncFailureSince to be reset")
			}

			cond := getNodeCACondition(t, regClient, "NodeCADaemonControllerDegraded")
			if tt.expectDegradedStatus == nil {
				if cond != nil && cond.Status == operatorv1.ConditionTrue {
					t.Errorf("expected NodeCADaemonControllerDegraded to not be True, got: %+v", cond)
				}
			} else {
				if cond == nil {
					t.Fatal("expected NodeCADaemonControllerDegraded condition to exist")
				}
				if cond.Status != *tt.expectDegradedStatus {
					t.Errorf("expected NodeCADaemonControllerDegraded=%s, got %s", *tt.expectDegradedStatus, cond.Status)
				}
			}
		})
	}
}

func TestNodeCADaemonControllerConditionsDuringInertia(t *testing.T) {
	controller, regClient := newNodeCATestSetup(t, fmt.Errorf("simulated API server error"))
	controller.syncFailureSince = time.Now()

	err := controller.sync()
	if err == nil {
		t.Fatal("expected sync to return an error")
	}

	available := getNodeCACondition(t, regClient, "NodeCADaemonAvailable")
	if available == nil {
		t.Fatal("expected NodeCADaemonAvailable condition to exist")
	}
	if available.Status != operatorv1.ConditionTrue {
		t.Errorf("expected NodeCADaemonAvailable=True, got %s", available.Status)
	}

	progressing := getNodeCACondition(t, regClient, "NodeCADaemonProgressing")
	if progressing == nil {
		t.Fatal("expected NodeCADaemonProgressing condition to exist")
	}
	if progressing.Status != operatorv1.ConditionFalse {
		t.Errorf("expected NodeCADaemonProgressing=False, got %s", progressing.Status)
	}

	degraded := getNodeCACondition(t, regClient, "NodeCADaemonControllerDegraded")
	if degraded != nil && degraded.Status == operatorv1.ConditionTrue {
		t.Errorf("expected NodeCADaemonControllerDegraded to not be True during inertia window, got: %+v", degraded)
	}
}

func newNodeCATestSetup(t *testing.T, applyError error) (*NodeCADaemonController, *imageregistryfakeclient.Clientset) {
	t.Helper()

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "node-ca",
			Namespace: defaults.ImageRegistryOperatorNamespace,
		},
		Status: appsv1.DaemonSetStatus{
			NumberAvailable:        3,
			DesiredNumberScheduled: 3,
			UpdatedNumberScheduled: 3,
		},
	}
	controller, kubeClient, regClient := newNodeCATestSetupWithCustomDaemonset(t, ds)
	if applyError != nil {
		failDaemonSetWrite := func(action clientgotesting.Action) (bool, runtime.Object, error) {
			return true, nil, applyError
		}
		kubeClient.PrependReactor("create", "daemonsets", failDaemonSetWrite)
		kubeClient.PrependReactor("update", "daemonsets", failDaemonSetWrite)
	}
	return controller, regClient
}

func newNodeCATestSetupWithCustomDaemonset(t *testing.T, ds *appsv1.DaemonSet) (*NodeCADaemonController, *kubefakeclient.Clientset, *imageregistryfakeclient.Clientset) {
	t.Helper()

	kubeClient := kubefakeclient.NewClientset(ds)
	regClient := imageregistryfakeclient.NewClientset(&imageregistryv1.Config{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	})
	regClient.PrependReactor("update", "configs", registryConfigResourceVersionBumper(t, regClient))

	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 0)
	regInformers := imageregistryinformers.NewSharedInformerFactory(regClient, 0)

	operatorClient := client.NewConfigOperatorClient(
		regClient.ImageregistryV1().Configs(),
		regInformers.Imageregistry().V1().Configs(),
	)

	daemonSetLister := kubeInformers.Apps().V1().DaemonSets().Lister().DaemonSets(defaults.ImageRegistryOperatorNamespace)
	serviceLister := kubeInformers.Core().V1().Services().Lister().Services(defaults.ImageRegistryOperatorNamespace)

	ctx := t.Context()
	kubeInformers.Start(ctx.Done())
	regInformers.Start(ctx.Done())
	kubeInformers.WaitForCacheSync(ctx.Done())
	regInformers.WaitForCacheSync(ctx.Done())

	controller := &NodeCADaemonController{
		eventRecorder:   events.NewInMemoryRecorder("test", clock.RealClock{}),
		appsClient:      kubeClient.AppsV1(),
		operatorClient:  operatorClient,
		daemonSetLister: daemonSetLister,
		serviceLister:   serviceLister,
	}

	return controller, kubeClient, regClient
}

func TestNodeCADaemonControllerProgressing(t *testing.T) {
	tests := []struct {
		name                          string
		dsStatus                      appsv1.DaemonSetStatus
		metadataGeneration            int64
		lastSettledObservedGeneration int64
		expectProgressing             operatorv1.ConditionStatus
		expectSettledGeneration       int64
	}{
		{
			name: "operator starting with progressing daemonset",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     1,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 1,
				UpdatedNumberScheduled: 1,
				NumberAvailable:        1,
			},
			metadataGeneration:            1,
			lastSettledObservedGeneration: 0,
			expectProgressing:             operatorv1.ConditionTrue,
			expectSettledGeneration:       0,
		},
		{
			name: "operator starting with settled daemonset",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     5,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 3,
				NumberAvailable:        3,
			},
			metadataGeneration:            5,
			lastSettledObservedGeneration: 0,
			expectProgressing:             operatorv1.ConditionFalse,
			expectSettledGeneration:       5,
		},
		{
			name: "slow daemonset controller",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     5,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 3,
				NumberAvailable:        3,
			},
			metadataGeneration:            6,
			lastSettledObservedGeneration: 5,
			expectProgressing:             operatorv1.ConditionTrue,
			expectSettledGeneration:       5,
		},
		{
			name: "progressing after spec change",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     2,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 1,
				NumberAvailable:        3,
			},
			metadataGeneration:            2,
			lastSettledObservedGeneration: 1,
			expectProgressing:             operatorv1.ConditionTrue,
			expectSettledGeneration:       1,
		},
		{
			name: "operator finds a new settled generation",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     8,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 3,
				NumberAvailable:        3,
			},
			metadataGeneration:            8,
			lastSettledObservedGeneration: 1,
			expectProgressing:             operatorv1.ConditionFalse,
			expectSettledGeneration:       8,
		},
		{
			name: "operator finds misscheduled pod",
			dsStatus: appsv1.DaemonSetStatus{
				ObservedGeneration:     8,
				DesiredNumberScheduled: 3,
				CurrentNumberScheduled: 3,
				UpdatedNumberScheduled: 3,
				NumberAvailable:        3,
				NumberMisscheduled:     1,
			},
			metadataGeneration:            8,
			lastSettledObservedGeneration: 8,
			expectProgressing:             operatorv1.ConditionTrue,
			expectSettledGeneration:       8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds := &appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "node-ca",
					Namespace:  defaults.ImageRegistryOperatorNamespace,
					Generation: tt.metadataGeneration,
				},
				Status: tt.dsStatus,
			}
			controller, _, regClient := newNodeCATestSetupWithCustomDaemonset(t, ds)
			controller.lastSettledObservedGeneration = tt.lastSettledObservedGeneration

			if err := controller.sync(); err != nil {
				t.Fatalf("unexpected sync error: %v", err)
			}

			progressing := getNodeCACondition(t, regClient, "NodeCADaemonProgressing")
			if progressing == nil {
				t.Fatal("expected NodeCADaemonProgressing condition to exist")
			}
			if progressing.Status != tt.expectProgressing {
				t.Errorf("expected NodeCADaemonProgressing=%s, got %s", tt.expectProgressing, progressing.Status)
			}
			if controller.lastSettledObservedGeneration != tt.expectSettledGeneration {
				t.Errorf("expected lastSettledObservedGeneration=%d, got %d", tt.expectSettledGeneration, controller.lastSettledObservedGeneration)
			}
		})
	}
}

func getNodeCACondition(t *testing.T, regClient *imageregistryfakeclient.Clientset, condType string) *operatorv1.OperatorCondition {
	t.Helper()

	cfg, err := regClient.ImageregistryV1().Configs().Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	for _, c := range cfg.Status.Conditions {
		if c.Type == condType {
			return &c
		}
	}
	return nil
}
