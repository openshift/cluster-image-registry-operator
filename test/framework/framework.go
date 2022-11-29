package framework

import (
	"fmt"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// AsyncOperationTimeout is how long we want to wait for asynchronous
// operations to complete. ForeverTestTimeout is not long enough to create
// several replicas and get them available on a slow machine.
var AsyncOperationTimeout = 5 * time.Minute

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

// DumpYAML prints the object to the test log as YAML.
func DumpYAML(logger Logger, prefix string, obj interface{}) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal object: %s", err))
	}
	logger.Logf("%s:\n%s", prefix, string(data))
}

// WaitUntilFinalized waits until obj is finalized. It expects getObject to
// return the up-to-date version of obj.
func WaitUntilFinalized(obj metav1.Object, getObject func() (metav1.Object, error)) error {
	uid := obj.GetUID()

	return wait.Poll(1*time.Second, AsyncOperationTimeout, func() (stop bool, err error) {
		obj, err := getObject()
		if errors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}

		if obj.GetUID() != uid {
			// the old object is finalized and a new one is created
			return true, nil
		}

		if obj.GetDeletionTimestamp() == nil {
			return false, fmt.Errorf("waiting until %T %s/%s (%s) is finalized, but its is not deleted", obj, obj.GetNamespace(), obj.GetName(), uid)
		}

		return false, nil
	})
}

// DeleteCompletely sends a delete request and waits until the resource and
// its dependents are deleted.
func DeleteCompletely(getObject func() (metav1.Object, error), deleteObject func(metav1.DeleteOptions) error) error {
	obj, err := getObject()
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	uid := obj.GetUID()
	policy := metav1.DeletePropagationForeground
	err = deleteObject(metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{
			UID: &uid,
		},
		PropagationPolicy: &policy,
	})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	return WaitUntilFinalized(obj, getObject)
}
