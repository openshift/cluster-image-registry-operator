package e2e

import (
	"os"
	"testing"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
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

	client, err := framework.NewClientset(nil)
	if err != nil {
		panic(err)
	}

	if err := framework.DisableCVOForOperator(devnullLogger{}, client); err != nil {
		panic(err)
	}

	if err := framework.RemoveImageRegistry(devnullLogger{}, client); err != nil {
		panic(err)
	}

	os.Exit(m.Run())
}
