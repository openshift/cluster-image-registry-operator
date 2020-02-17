package framework

import "testing"

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

	return &testEnv{
		T:      t,
		client: client,
	}
}

func (te *testEnv) Client() *Clientset {
	return te.client
}
