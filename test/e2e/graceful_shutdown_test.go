package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift/cluster-image-registry-operator/defaults"
	"github.com/openshift/cluster-image-registry-operator/test/framework"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeCAGracefulShutdown(t *testing.T) {
	client := framework.MustNewClientset(t, nil)
	framework.MustEnsureNodeCADaemonSetIsAvailable(t, client)

	pods, err := client.Pods(defaults.ImageRegistryOperatorNamespace).List(
		metav1.ListOptions{
			LabelSelector: "name=node-ca",
		},
	)
	if err != nil {
		t.Fatalf("unable to list pods: %v", err)
	}

	// selects pod to kill. MustEnsureNodeCADaemonSetIsAvailable guarantees
	// we have at least one pod available.
	var pod corev1.Pod
	for _, p := range pods.Items {
		if p.Status.Phase != "Running" {
			continue
		}
		pod = p
		break
	}

	logch, errch := framework.MustFollowPodLog(t, pod)

	if err := client.Pods(defaults.ImageRegistryOperatorNamespace).Delete(
		pod.Name,
		&metav1.DeleteOptions{},
	); err != nil {
		t.Fatalf("error deleting pod: %v", err)
	}

	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("timeout awaiting for pod to die.")
		case err, more := <-errch:
			if !more {
				errch = nil
				break
			}
			t.Fatalf("error reading pod log: %v", err)
		case line, more := <-logch:
			if !more {
				t.Fatal("pod died, no graceful message found.")
			}
			// this is the log line node-ca pods print when they
			// gorgeously die, if we find this it means that the
			// pod had manage to exit properly so we can end this
			// test successfuly.
			if strings.HasPrefix(line, "shutting down node-ca") {
				return
			}
		}
	}
}

func TestImageRegistryGracefulShutdown(t *testing.T) {
	client := framework.MustNewClientset(t, nil)
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureOperatorIsNotHotLooping(t, client)

	pods, err := client.Pods(defaults.ImageRegistryOperatorNamespace).List(
		metav1.ListOptions{
			LabelSelector: "docker-registry=default",
		},
	)
	if err != nil {
		t.Fatalf("unable to list pods: %v", err)
	}

	// selects pod to kill. MustEnsureImageRegistryIsAvailable guarantees
	// we have at least one pod available.
	var pod corev1.Pod
	for _, p := range pods.Items {
		if p.Status.Phase != "Running" {
			continue
		}
		pod = p
		break
	}

	logch, errch := framework.MustFollowPodLog(t, pod)

	if err := client.Pods(defaults.ImageRegistryOperatorNamespace).Delete(
		pod.Name,
		&metav1.DeleteOptions{},
	); err != nil {
		t.Fatalf("error deleting pod: %v", err)
	}

	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()
	for {
		select {
		case <-timeout.C:
			t.Fatal("timeout awaiting for pod to die.")
		case err, more := <-errch:
			if !more {
				errch = nil
				break
			}
			t.Fatalf("error reading pod log: %v", err)
		case line, more := <-logch:
			if !more {
				t.Fatal("pod died, no graceful message found.")
			}
			// this is the log line image registry pod prints when
			// it gracefuly dies, if we find this it means that the
			// pod had manage to exit properly so we can end this
			// test successfuly.
			if strings.Contains(line, "server shutdown, bye.") {
				return
			}
		}
	}
}
