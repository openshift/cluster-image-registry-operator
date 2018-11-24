package controller

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

var (
	DefaultResyncDuration time.Duration = 10 * time.Minute
)

type Handler func(action string, o interface{})

type Watcher interface {
	Start(handler Handler, namespace string, stopCh <-chan struct{}) error
	Get(name, namespace string) (runtime.Object, error)
}
