package framework

import (
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
	*testing.T
	client *Clientset
}

func Setup(t *testing.T) TestEnv {
	client, err := NewClientset(nil)
	if err != nil {
		t.Fatal(err)
	}

	te := &testEnv{
		T:      t,
		client: client,
	}
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
	te.T.Helper()
	args := append([]interface{}{te.timestamp()}, a...)
	te.T.Log(args...)
}

func (te *testEnv) Logf(format string, a ...interface{}) {
	te.T.Helper()
	args := append([]interface{}{te.timestamp()}, a...)
	te.T.Logf("%s "+format, args...)
}
