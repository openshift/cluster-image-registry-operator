// Package e2e contains end-to-end tests for the cluster-image-registry-operator.
package e2e

import (
	"fmt"
	"os"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

type bootstrapTestEnv struct {
	client *framework.Clientset
}

func (te *bootstrapTestEnv) Client() *framework.Clientset { return te.client }
func (te *bootstrapTestEnv) Failed() bool                 { return false }
func (te *bootstrapTestEnv) Log(a ...interface{})         { fmt.Fprintln(os.Stderr, a...) }
func (te *bootstrapTestEnv) Logf(f string, a ...interface{}) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
}
func (te *bootstrapTestEnv) Error(a ...interface{})            { panic(fmt.Sprint(a...)) }
func (te *bootstrapTestEnv) Errorf(f string, a ...interface{}) { panic(fmt.Sprintf(f, a...)) }
func (te *bootstrapTestEnv) Fatal(a ...interface{})            { panic(fmt.Sprint(a...)) }
func (te *bootstrapTestEnv) Fatalf(f string, a ...interface{}) { panic(fmt.Sprintf(f, a...)) }

var _ = g.BeforeSuite(func() {
	if os.Getenv("KUBERNETES_CONFIG") == "" {
		kubeConfig := os.Getenv("KUBECONFIG")
		if kubeConfig == "" {
			kubeConfig = os.Getenv("HOME") + "/.kube/config"
		}
		os.Setenv("KUBERNETES_CONFIG", kubeConfig)
	}

	client, err := framework.NewClientset(nil)
	o.Expect(err).NotTo(o.HaveOccurred(), "failed to instantiate clientset")

	te := &bootstrapTestEnv{client: client}

	framework.DisableCVOForOperator(te)
	framework.RemoveImageRegistry(te)
})
