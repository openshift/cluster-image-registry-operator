package wrapped

import (
	"context"
	"io"

	storagedriver "github.com/docker/distribution/registry/storage/driver"
)

// storageDriver wraps a distribution/registry/storage/driver.StorageDriver.
type storageDriver struct {
	storageDriver storagedriver.StorageDriver
	wrapper       SimpleWrapper
}

var _ storagedriver.StorageDriver = &storageDriver{}

func NewStorageDriver(driver storagedriver.StorageDriver, wrapper SimpleWrapper) storagedriver.StorageDriver {
	return &storageDriver{
		storageDriver: driver,
		wrapper:       wrapper,
	}
}

func (d *storageDriver) Name() string {
	return d.storageDriver.Name()
}

func (d *storageDriver) GetContent(ctx context.Context, path string) (content []byte, err error) {
	err = d.wrapper("StorageDriver.GetContent", func() error {
		content, err = d.storageDriver.GetContent(ctx, path)
		return err
	})
	return
}

func (d *storageDriver) PutContent(ctx context.Context, path string, content []byte) error {
	return d.wrapper("StorageDriver.PutContent", func() error {
		return d.storageDriver.PutContent(ctx, path, content)
	})
}

func (d *storageDriver) Reader(ctx context.Context, path string, offset int64) (r io.ReadCloser, err error) {
	err = d.wrapper("StorageDriver.Reader", func() error {
		r, err = d.storageDriver.Reader(ctx, path, offset)
		if err == nil {
			r = &readCloser{
				readCloser: r,
				wrapper:    d.wrapper,
			}
		}
		return err
	})
	return
}

func (d *storageDriver) Writer(ctx context.Context, path string, append bool) (w storagedriver.FileWriter, err error) {
	err = d.wrapper("StorageDriver.Writer", func() error {
		w, err = d.storageDriver.Writer(ctx, path, append)
		if err == nil {
			w = &fileWriter{
				fileWriter: w,
				wrapper:    d.wrapper,
			}
		}
		return err
	})
	return
}

func (d *storageDriver) Stat(ctx context.Context, path string) (fi storagedriver.FileInfo, err error) {
	err = d.wrapper("StorageDriver.Stat", func() error {
		fi, err = d.storageDriver.Stat(ctx, path)
		return err
	})
	return
}

func (d *storageDriver) List(ctx context.Context, path string) (entries []string, err error) {
	err = d.wrapper("StorageDriver.List", func() error {
		entries, err = d.storageDriver.List(ctx, path)
		return err
	})
	return
}

func (d *storageDriver) Move(ctx context.Context, sourcePath string, destPath string) error {
	return d.wrapper("StorageDriver.Move", func() error {
		return d.storageDriver.Move(ctx, sourcePath, destPath)
	})
}

func (d *storageDriver) Delete(ctx context.Context, path string) error {
	return d.wrapper("StorageDriver.Delete", func() error {
		return d.storageDriver.Delete(ctx, path)
	})
}

func (d *storageDriver) URLFor(ctx context.Context, path string, options map[string]interface{}) (url string, err error) {
	err = d.wrapper("StorageDriver.URLFor", func() error {
		url, err = d.storageDriver.URLFor(ctx, path, options)
		return err
	})
	return
}

func (d *storageDriver) Walk(ctx context.Context, path string, f storagedriver.WalkFn) error {
	return d.wrapper("StorageDriver.Walk", func() error {
		return d.storageDriver.Walk(ctx, path, f)
	})
}
