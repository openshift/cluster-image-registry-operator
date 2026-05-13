package framework

import (
	"os"
	"sync"
	"testing"
	"time"
)

type TestEnv interface {
	Client() *Clientset
	Failed() bool
	Log(a ...interface{})
	Logf(format string, a ...interface{})
	Error(a ...interface{})
	Errorf(format string, a ...interface{})
	Fatal(a ...interface{})
	Fatalf(format string, a ...interface{})
}

type testEnv struct {
	testing.TB
	client *Clientset
}

var initOnce sync.Once

// initTestEnvironment performs one-time cluster preparation: disables CVO
// management of the operator and removes the image registry so that tests
// can control the operator lifecycle directly.
func initTestEnvironment(te TestEnv) {
	if os.Getenv("KUBERNETES_CONFIG") == "" {
		kubeConfig := os.Getenv("KUBECONFIG")
		if kubeConfig == "" {
			kubeConfig = os.Getenv("HOME") + "/.kube/config"
		}
		os.Setenv("KUBERNETES_CONFIG", kubeConfig)
	}

	DisableCVOForOperator(te)
	RemoveImageRegistry(te)
}

func Setup(t testing.TB) TestEnv {
	client, err := NewClientset(nil)
	if err != nil {
		t.Fatal(err)
	}

	te := &testEnv{
		TB:     t,
		client: client,
	}
	initOnce.Do(func() {
		initTestEnvironment(te)
	})
	CheckAbsenceOfOperatorPods(te)
	return te
}

func (te *testEnv) timestamp() string {
	return time.Now().UTC().Format("15:04:05.000")
}

func (te *testEnv) Client() *Clientset {
	return te.client
}

func (te *testEnv) Log(a ...interface{}) {
	te.TB.Helper()
	args := append([]interface{}{te.timestamp()}, a...)
	te.TB.Log(args...)
}

func (te *testEnv) Logf(format string, a ...interface{}) {
	te.TB.Helper()
	args := append([]interface{}{te.timestamp()}, a...)
	te.TB.Logf("%s "+format, args...)
}
