package storagepath_test

import (
	"context"
	"io"
	"testing"

	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/storage"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	"github.com/docker/distribution/registry/storage/driver/inmemory"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/testutil/logger"
	"github.com/openshift/image-registry/test/internal/storagepath"
)

type storageDriver struct {
	storageDriver storagedriver.StorageDriver
	logger        logger.Logger
}

var _ storagedriver.StorageDriver = &storageDriver{}

func (sd *storageDriver) Name() string {
	return sd.storageDriver.Name()
}

func (sd *storageDriver) GetContent(ctx context.Context, path string) ([]byte, error) {
	sd.logger.Printf("GET %s", path)
	return sd.storageDriver.GetContent(ctx, path)
}

func (sd *storageDriver) PutContent(ctx context.Context, path string, content []byte) error {
	sd.logger.Printf("PUT %s", path)
	return sd.storageDriver.PutContent(ctx, path, content)
}

func (sd *storageDriver) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	sd.logger.Printf("READ %s", path)
	return sd.storageDriver.Reader(ctx, path, offset)
}

func (sd *storageDriver) Writer(ctx context.Context, path string, append bool) (storagedriver.FileWriter, error) {
	sd.logger.Printf("WRITE %s", path)
	return sd.storageDriver.Writer(ctx, path, append)
}

func (sd *storageDriver) Stat(ctx context.Context, path string) (storagedriver.FileInfo, error) {
	sd.logger.Printf("STAT %s", path)
	return sd.storageDriver.Stat(ctx, path)
}

func (sd *storageDriver) List(ctx context.Context, path string) ([]string, error) {
	sd.logger.Printf("LIST %s", path)
	return sd.storageDriver.List(ctx, path)
}

func (sd *storageDriver) Move(ctx context.Context, sourcePath string, destPath string) error {
	sd.logger.Printf("MOVE %s %s", sourcePath, destPath)
	return sd.storageDriver.Move(ctx, sourcePath, destPath)
}

func (sd *storageDriver) Delete(ctx context.Context, path string) error {
	sd.logger.Printf("DELETE %s", path)
	return sd.storageDriver.Delete(ctx, path)
}

func (sd *storageDriver) URLFor(ctx context.Context, path string, options map[string]interface{}) (string, error) {
	sd.logger.Printf("URL %s", path)
	return sd.storageDriver.URLFor(ctx, path, options)
}

func (sd *storageDriver) Walk(ctx context.Context, path string, f storagedriver.WalkFn) error {
	sd.logger.Printf("WALK %s", path)
	return sd.storageDriver.Walk(ctx, path, f)
}

func TestManifest(t *testing.T) {
	const reponame = "foo/bar"

	ctx := context.Background()
	logger := logger.New()
	driver := &storageDriver{
		storageDriver: inmemory.New(),
		logger:        logger,
	}

	reg, err := storage.NewRegistry(ctx, driver)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := reference.WithName(reponame)
	if err != nil {
		t.Fatal(err)
	}

	repo, err := reg.Repository(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}

	ms, err := repo.Manifests(ctx)
	if err != nil {
		t.Fatal(err)
	}

	dgst := digest.Digest("sha256:0000000000000000000000000000000000000000000000000000000000000001")

	manifest, err := ms.Get(ctx, dgst)
	if err == nil {
		t.Fatalf("got a manifest from the empty storage: %v", manifest)
	}

	if err := logger.Compare([]string{
		"GET " + storagepath.Manifest(reponame, dgst),
		// There is an old version of Distribution that writes manifest links to a wrong place.
		"GET " + storagepath.Layer(reponame, dgst),
	}); err != nil {
		t.Fatal(err)
	}
}
