package server

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"github.com/openshift/image-registry/pkg/imagestream"
)

// projectObjectListCache implements projectObjectListStore.
type projectObjectListCache struct {
	store cache.Store
}

var _ imagestream.ProjectObjectListStore = &projectObjectListCache{}

// newProjectObjectListCache creates a cache to hold object list objects that will expire with the given ttl.
func newProjectObjectListCache(ttl time.Duration) imagestream.ProjectObjectListStore {
	return &projectObjectListCache{
		store: cache.NewTTLStore(metaProjectObjectListKeyFunc, ttl),
	}
}

// add stores given list object under the given namespace. Any prior object under this
// key will be replaced.
func (c *projectObjectListCache) Add(namespace string, obj runtime.Object) error {
	if namespace == "" {
		return fmt.Errorf("namespace cannot be empty")
	}
	no := &namespacedObject{
		namespace: namespace,
		object:    obj,
	}
	return c.store.Add(no)
}

// get retrieves a cached list object if present and not expired.
func (c *projectObjectListCache) Get(namespace string) (runtime.Object, bool, error) {
	entry, exists, err := c.store.GetByKey(namespace)
	if err != nil {
		return nil, exists, err
	}
	if !exists {
		return nil, false, err
	}
	no, ok := entry.(*namespacedObject)
	if !ok {
		return nil, false, fmt.Errorf("%T is not a namespaced object", entry)
	}
	return no.object, true, nil
}

// namespacedObject is a container associating an object with a namespace.
type namespacedObject struct {
	namespace string
	object    runtime.Object
}

// metaProjectObjectListKeyFunc returns a key for given namespaced object. The key is object's namespace.
func metaProjectObjectListKeyFunc(obj interface{}) (string, error) {
	if key, ok := obj.(cache.ExplicitKey); ok {
		return string(key), nil
	}
	no, ok := obj.(*namespacedObject)
	if !ok {
		return "", fmt.Errorf("object %T is not a namespaced object", obj)
	}
	return no.namespace, nil
}
