package supermiddleware

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/docker/distribution"
	"github.com/docker/distribution/configuration"
	"github.com/docker/distribution/reference"
	"github.com/docker/distribution/registry/auth"
	"github.com/docker/distribution/registry/storage/cache"
	storagedriver "github.com/docker/distribution/registry/storage/driver"
	_ "github.com/docker/distribution/registry/storage/driver/inmemory"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/wrapped"
)

type log struct {
	records []string
}

func (l *log) Reset() {
	l.records = nil
}

func (l *log) Record(format string, v ...interface{}) {
	l.records = append(l.records, fmt.Sprintf(format, v...))
}

func (l *log) Compare(expected []string) error {
	if len(l.records) == 0 && len(expected) == 0 {
		return nil
	}
	if !reflect.DeepEqual(l.records, expected) {
		return fmt.Errorf("got %v, want %v", l.records, expected)
	}
	return nil
}

type testAccessController struct {
	log *log
}

func (ac *testAccessController) Authorized(ctx context.Context, access ...auth.Access) (context.Context, error) {
	var args []string
	for _, a := range access {
		args = append(args, fmt.Sprintf("%s:%s:%s:%s", a.Type, a.Class, a.Name, a.Action))
	}
	ac.log.Record("AccessController(%s)", strings.Join(args, ", "))
	return ctx, nil
}

type testRegistry struct {
	distribution.Namespace
	log *log
}

func (reg *testRegistry) Repository(ctx context.Context, named reference.Named) (distribution.Repository, error) {
	reg.log.Record("%s: enter Registry.Repository", named)
	defer reg.log.Record("%s: leave Registry.Repository", named)
	return reg.Namespace.Repository(ctx, named)
}

type testApp struct {
	log *log
}

func (app *testApp) Auth(options map[string]interface{}) (auth.AccessController, error) {
	return &testAccessController{
		log: app.log,
	}, nil
}

func (app *testApp) Storage(driver storagedriver.StorageDriver, options map[string]interface{}) (storagedriver.StorageDriver, error) {
	return driver, nil
}

func (app *testApp) Registry(registry distribution.Namespace, options map[string]interface{}) (distribution.Namespace, error) {
	return &testRegistry{
		Namespace: registry,
		log:       app.log,
	}, nil
}

func (app *testApp) Repository(ctx context.Context, repo distribution.Repository, crossmount bool) (distribution.Repository, distribution.BlobDescriptorServiceFactory, error) {
	name := "regular"
	if crossmount {
		name = "crossmount"
	}

	wrapper := func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		app.log.Record("%s(%s): enter %s", repo.Named(), name, funcname)
		defer app.log.Record("%s(%s): leave %s", repo.Named(), name, funcname)
		return f(ctx)
	}

	repo = wrapped.NewRepository(repo, wrapper)
	bdsf := blobDescriptorServiceFactoryFunc(func(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService {
		return wrapped.NewBlobDescriptorService(svc, wrapper)
	})

	return repo, bdsf, nil
}

func (app *testApp) CacheProvider(ctx context.Context, options map[string]interface{}) (cache.BlobDescriptorCacheProvider, error) {
	return nil, nil
}

func TestApp(t *testing.T) {
	log := &log{}

	ctx := context.Background()
	app := &testApp{log: log}
	config := &configuration.Configuration{
		Auth: configuration.Auth{
			Name: nil,
		},
		Storage: configuration.Storage{
			"inmemory": nil,
			"delete": configuration.Parameters{
				"enabled": true,
			},
			"cache": configuration.Parameters{
				"blobdescriptor": Name,
			},
		},
		Middleware: map[string][]configuration.Middleware{
			"registry":   {{Name: Name}},
			"repository": {{Name: Name}},
			"storage":    {{Name: Name}},
		},
	}
	handler := NewApp(ctx, config, app)

	server := httptest.NewServer(handler)
	defer server.Close()

	fooDigest := "sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
	fooContent := []byte("foo")

	var location string
	serverURL := func(s string) func() string { return func() string { return server.URL + s } }
	lastLocation := func(s string) func() string { return func() string { return location + s } }

	for _, test := range []struct {
		name         string
		method       string
		url          func() string
		body         io.Reader
		expectStatus int
		expectLog    []string
	}{
		{
			name:         "foo_get_blob",
			method:       "HEAD",
			url:          serverURL("/v2/foo/blobs/" + fooDigest + "/"),
			expectStatus: http.StatusNotFound,
			expectLog: []string{
				"AccessController(repository::foo:pull)",
				"foo: enter Registry.Repository",
				"foo: leave Registry.Repository",
				"foo(regular): enter BlobStore.Stat",
				"foo(regular): enter BlobDescriptorService.Stat",
				"foo(regular): leave BlobDescriptorService.Stat",
				"foo(regular): leave BlobStore.Stat",
			},
		},
		{
			name:         "foo_start_blob_upload",
			method:       "POST",
			url:          serverURL("/v2/foo/blobs/uploads/"),
			expectStatus: http.StatusAccepted,
			expectLog: []string{
				"AccessController(repository::foo:pull, repository::foo:push)",
				"foo: enter Registry.Repository",
				"foo: leave Registry.Repository",
				"foo(regular): enter BlobStore.Create",
				"foo(regular): leave BlobStore.Create",
			},
		},
		{
			name:         "foo_put_blob_foo",
			method:       "PUT",
			url:          lastLocation("&digest=" + fooDigest),
			body:         bytes.NewReader(fooContent),
			expectStatus: http.StatusCreated,
			expectLog: []string{
				"AccessController(repository::foo:pull, repository::foo:push)",
				"foo: enter Registry.Repository",
				"foo: leave Registry.Repository",
				"foo(regular): enter BlobStore.Resume",
				"foo(regular): leave BlobStore.Resume",
				"foo(regular): enter BlobWriter.Commit",
				"foo(regular): enter BlobDescriptorService.SetDescriptor",
				"foo(regular): leave BlobDescriptorService.SetDescriptor",
				"foo(regular): leave BlobWriter.Commit",
			},
		},
		{
			name:         "bar_mount_blob",
			method:       "POST",
			url:          serverURL("/v2/bar/blobs/uploads/?mount=" + fooDigest + "&from=foo"),
			expectStatus: http.StatusCreated,
			expectLog: []string{
				"AccessController(repository::bar:pull, repository::bar:push, repository::foo:pull)",
				"bar: enter Registry.Repository",
				"bar: leave Registry.Repository",
				"bar(regular): enter BlobStore.Create",
				"foo(crossmount): enter BlobDescriptorService.Stat",
				"foo(crossmount): leave BlobDescriptorService.Stat",
				"bar(regular): leave BlobStore.Create",
			},
		},
		{
			name:         "foo_delete_blob",
			method:       "DELETE",
			url:          serverURL("/v2/foo/blobs/" + fooDigest),
			expectStatus: http.StatusAccepted,
			expectLog: []string{
				"AccessController(repository::foo:delete)",
				"foo: enter Registry.Repository",
				"foo: leave Registry.Repository",
				"foo(regular): enter BlobStore.Delete",
				"foo(regular): enter BlobDescriptorService.Stat",
				"foo(regular): leave BlobDescriptorService.Stat",
				"foo(regular): enter BlobDescriptorService.Clear",
				"foo(regular): leave BlobDescriptorService.Clear",
				"foo(regular): leave BlobStore.Delete",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			req, err := http.NewRequest(test.method, test.url(), test.body)
			if err != nil {
				t.Fatal(err)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			location = resp.Header.Get("Location")

			if resp.StatusCode != test.expectStatus {
				t.Errorf("got status %d (%s), want %d", resp.StatusCode, resp.Status, test.expectStatus)
			}

			if err := log.Compare(test.expectLog); err != nil {
				t.Fatal(err)
			}
			log.Reset()
		})
	}
}
