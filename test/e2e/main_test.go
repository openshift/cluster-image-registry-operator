package e2e_test

import (
	"os"
	"testing"

	"github.com/openshift/cluster-image-registry-operator/pkg/testframework"
)

type devnullLogger struct{}

func (_ devnullLogger) Logf(string, ...interface{}) {}

func TestMain(m *testing.M) {
	if os.Getenv("KUBERNETES_CONFIG") == "" {
		kubeConfig := os.Getenv("KUBECONFIG")
		if kubeConfig == "" {
			kubeConfig = os.Getenv("HOME") + "/.kube/config"
		}
		os.Setenv("KUBERNETES_CONFIG", kubeConfig)
	}

	client, err := testframework.NewClientset(nil)
	if err != nil {
		panic(err)
	}

	if err := testframework.DisableCVOForOperator(client); err != nil {
		panic(err)
	}

	if err := testframework.RemoveImageRegistry(devnullLogger{}, client); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
