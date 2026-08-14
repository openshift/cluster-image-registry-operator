package e2e

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// Set KUBERNETES_CONFIG for legacy go test execution
	// (Ginkgo execution uses main.go BeforeSuite instead)
	if os.Getenv("KUBERNETES_CONFIG") == "" {
		kubeConfig := os.Getenv("KUBECONFIG")
		if kubeConfig == "" {
			kubeConfig = os.Getenv("HOME") + "/.kube/config"
		}
		os.Setenv("KUBERNETES_CONFIG", kubeConfig)
	}

	os.Exit(m.Run())
}
