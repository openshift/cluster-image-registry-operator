package testframework

import (
	"fmt"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	// AsyncOperationTimeout is how long we want to wait for asynchronous
	// operations to complete. ForeverTestTimeout is not long enough to create
	// several replicas and get them available on a slow machine.
	// Setting this to 5 minutes:w

	AsyncOperationTimeout = 5 * time.Minute
)

// Logger is an interface to report events from tests. It is implemented by
// testing.T.
type Logger interface {
	Logf(string, ...interface{})
}

var _ Logger = &testing.T{}

// loopLogger hides repeated messages from the log.
type loopLogger struct {
	logger   Logger
	count    int
	prevMsg  string
	prevTime string
}

func newLoopLogger(logger Logger) *loopLogger {
	return &loopLogger{
		logger:   logger,
		count:    0,
		prevMsg:  "",
		prevTime: "",
	}
}

func (l *loopLogger) time() string {
	return time.Now().Format("15:04:05")
}

func (l *loopLogger) Logf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if msg == l.prevMsg {
		l.count++
		l.prevTime = l.time()
		return
	}
	l.Flush()
	l.count = 1
	l.prevMsg = msg
	l.prevTime = l.time()
	if t, ok := l.logger.(*testing.T); ok {
		t.Helper()
	}
	l.logger.Logf("%s %s", l.prevTime, l.prevMsg)
}

func (l *loopLogger) Flush() {
	if l.count > 1 {
		if t, ok := l.logger.(*testing.T); ok {
			t.Helper()
		}
		l.logger.Logf("%s %s (x%d)", l.prevTime, l.prevMsg, l.count-1)
	}
	l.count = 0
	l.prevMsg = ""
	l.prevTime = ""
}

// DumpObject prints the object to the test log.
func DumpObject(logger Logger, prefix string, obj interface{}) {
	logger.Logf("%s:\n%s", prefix, spew.Sdump(obj))
}

// DeleteCompletely sends a delete request and waits until the resource and
// its dependents are deleted.
func DeleteCompletely(getObject func() (metav1.Object, error), deleteObject func(*metav1.DeleteOptions) error) error {
	obj, err := getObject()
	if err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	uid := obj.GetUID()

	policy := metav1.DeletePropagationForeground
	if err := deleteObject(&metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID: &uid,
		},
		PropagationPolicy: &policy,
	}); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		obj, err = getObject()
		if err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}

		return obj.GetUID() != uid, nil
	})
}
