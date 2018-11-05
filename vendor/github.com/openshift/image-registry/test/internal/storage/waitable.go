package storage

import (
	"context"
	"fmt"
	"sync"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
)

type WaitableDriver interface {
	storagedriver.StorageDriver
	WaitFor(ctx context.Context, paths ...string) error
}

type driver struct {
	storagedriver.StorageDriver

	mu      sync.Mutex
	demands map[string]chan struct{}
}

var _ WaitableDriver = &driver{}

func NewWaitableDriver(sd storagedriver.StorageDriver) WaitableDriver {
	return &driver{
		StorageDriver: sd,
		demands:       make(map[string]chan struct{}),
	}
}

func (d *driver) WaitFor(ctx context.Context, paths ...string) error {
	type pending struct {
		path string
		c    <-chan struct{}
	}
	var queue []pending

	d.mu.Lock()
	for _, path := range paths {
		if _, err := d.Stat(ctx, path); err != nil {
			if _, ok := err.(storagedriver.PathNotFoundError); ok {
				c, ok := d.demands[path]
				if !ok {
					c = make(chan struct{})
					d.demands[path] = c
				}
				queue = append(queue, pending{path: path, c: c})
			} else {
				d.mu.Unlock()
				return fmt.Errorf("stat %s: %v", path, err)
			}
		}
	}
	d.mu.Unlock()

	for _, p := range queue {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for %s: %v", p.path, ctx.Err())
		case <-p.c:
		}
	}
	return nil
}

func (d *driver) PutContent(ctx context.Context, path string, content []byte) error {
	err := d.StorageDriver.PutContent(ctx, path, content)
	if err == nil {
		d.mu.Lock()
		c, ok := d.demands[path]
		if ok {
			close(c)
			delete(d.demands, path)
		}
		d.mu.Unlock()
	}
	return err
}
