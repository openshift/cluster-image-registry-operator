package e2e

import (
	"fmt"
	"time"

	g "github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

// ginkgoTestEnv adapts the framework.TestEnv interface for use within
// Ginkgo test specs. It creates a framework.Clientset and delegates
// logging and error reporting to Ginkgo.
type ginkgoTestEnv struct {
	client *framework.Clientset
	failed bool
}

var _ framework.TestEnv = &ginkgoTestEnv{}

// newGinkgoTestEnv creates a new ginkgoTestEnv backed by a real cluster
// client. It will cause a Ginkgo failure if the clientset can't be created.
//
// Unlike the classic e2e framework.Setup, this does NOT call
// CheckAbsenceOfOperatorPods because OTE tests run against a live
// cluster where the operator is already deployed and managing the
// image registry.
func newGinkgoTestEnv() *ginkgoTestEnv {
	client, err := framework.NewClientset(nil)
	if err != nil {
		g.Fail(fmt.Sprintf("unable to create clientset: %v", err))
	}

	return &ginkgoTestEnv{
		client: client,
	}
}

func (te *ginkgoTestEnv) timestamp() string {
	return time.Now().UTC().Format("15:04:05.000")
}

func (te *ginkgoTestEnv) Client() *framework.Clientset {
	return te.client
}

func (te *ginkgoTestEnv) Failed() bool {
	return te.failed
}

func (te *ginkgoTestEnv) Log(a ...interface{}) {
	args := append([]interface{}{te.timestamp()}, a...)
	g.GinkgoWriter.Println(args...)
}

func (te *ginkgoTestEnv) Logf(format string, a ...interface{}) {
	args := append([]interface{}{te.timestamp()}, a...)
	fmt.Fprintf(g.GinkgoWriter, "%s "+format+"\n", args...)
}

func (te *ginkgoTestEnv) Error(a ...interface{}) {
	te.failed = true
	args := append([]interface{}{te.timestamp()}, a...)
	g.GinkgoWriter.Println(args...)
	g.Fail(fmt.Sprint(a...))
}

func (te *ginkgoTestEnv) Errorf(format string, a ...interface{}) {
	te.failed = true
	args := append([]interface{}{te.timestamp()}, a...)
	fmt.Fprintf(g.GinkgoWriter, "%s "+format+"\n", args...)
	g.Fail(fmt.Sprintf(format, a...))
}

func (te *ginkgoTestEnv) Fatal(a ...interface{}) {
	te.failed = true
	args := append([]interface{}{te.timestamp()}, a...)
	g.GinkgoWriter.Println(args...)
	g.Fail(fmt.Sprint(a...))
}

func (te *ginkgoTestEnv) Fatalf(format string, a ...interface{}) {
	te.failed = true
	args := append([]interface{}{te.timestamp()}, a...)
	fmt.Fprintf(g.GinkgoWriter, "%s "+format+"\n", args...)
	g.Fail(fmt.Sprintf(format, a...))
}

// envToMap converts a slice of Kubernetes EnvVar to a map for easy lookup.
func envToMap(envVars []corev1.EnvVar) map[string]string {
	m := make(map[string]string, len(envVars))
	for _, env := range envVars {
		m[env.Name] = env.Value
	}
	return m
}
