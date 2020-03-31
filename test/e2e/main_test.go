package e2e

import (
	"fmt"
	"os"
	"testing"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

type bootstrapTestEnv struct {
	client *framework.Clientset
}

func (te *bootstrapTestEnv) Client() *framework.Clientset      { return te.client }
func (te *bootstrapTestEnv) Failed() bool                      { return false }
func (te *bootstrapTestEnv) Log(...interface{})                {}
func (te *bootstrapTestEnv) Logf(string, ...interface{})       {}
func (te *bootstrapTestEnv) Error(a ...interface{})            { panic(fmt.Sprint(a...)) }
func (te *bootstrapTestEnv) Errorf(f string, a ...interface{}) { panic(fmt.Sprintf(f, a...)) }
func (te *bootstrapTestEnv) Fatal(a ...interface{})            { panic(fmt.Sprint(a...)) }
func (te *bootstrapTestEnv) Fatalf(f string, a ...interface{}) { panic(fmt.Sprintf(f, a...)) }

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

	te := &bootstrapTestEnv{client: client}

	framework.DisableCVOForOperator(te)
	framework.RemoveImageRegistry(te)

	os.Exit(m.Run())
}
