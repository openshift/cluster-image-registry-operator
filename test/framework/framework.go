package framework

import (
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

// DumpObject prints the object to the test log.
func DumpObject(logger Logger, prefix string, obj interface{}) {
	logger.Logf("%s:\n%s", prefix, spew.Sdump(obj))
}

// DeleteCompletely sends a delete request that includes foreground deletion propagation.
// If waitForDelete is set to true, it will confirm via get operations that the object is
// deleted before returning.
func DeleteCompletely(getObject func() (metav1.Object, error), deleteObject func(*metav1.DeleteOptions) error, waitForDeletion bool) error {
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

	if !waitForDeletion {
		return nil
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
