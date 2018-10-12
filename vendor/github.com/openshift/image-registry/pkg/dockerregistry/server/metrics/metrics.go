package metrics

import (
	"context"
	"io"
	"net/url"
	"strings"

	"github.com/docker/distribution"
	"github.com/docker/distribution/registry/api/errcode"
	storagedriver "github.com/docker/distribution/registry/storage/driver"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/wrapped"
	"github.com/openshift/image-registry/pkg/origin-common/image/registryclient"
)

// Observer captures individual observations.
type Observer interface {
	Observe(float64)
}

// Counter represents a single numerical value that only goes up.
type Counter interface {
	Inc()
}

// Sink provides an interface for exposing metrics.
type Sink interface {
	RequestDuration(funcname, reponame string) Observer
	PullthroughBlobstoreCacheRequests(resultType string) Counter
	PullthroughRepositoryDuration(registry, funcname string) Observer
	PullthroughRepositoryErrors(registry, funcname, errcode string) Counter
	StorageDuration(funcname string) Observer
	StorageErrors(funcname, errcode string) Counter
	DigestCacheRequests(resultType string) Counter
	DigestCacheScopedRequests(resultType string) Counter
}

// Metrics is a set of all metrics that can be provided.
type Metrics interface {
	Core
	Pullthrough
	Storage
	DigestCache
}

// Core is a set of metrics for the core functionality.
type Core interface {
	// Repository wraps a distribution.Repository to collect statistics.
	Repository(r distribution.Repository, reponame string) distribution.Repository
}

// Pullthrough is a set of metrics for the pullthrough subsystem.
type Pullthrough interface {
	// RepositoryRetriever wraps RepositoryRetriever to collect statistics.
	RepositoryRetriever(retriever registryclient.RepositoryRetriever) registryclient.RepositoryRetriever

	// DigestBlobStoreCache() returns an interface to count cache hits/misses
	// for pullthrough blobstores.
	DigestBlobStoreCache() Cache
}

// Storage is a set of metrics for the storage subsystem.
type Storage interface {
	// StorageDriver wraps distribution/registry/storage/driver.StorageDriver
	// to collect statistics.
	StorageDriver(driver storagedriver.StorageDriver) storagedriver.StorageDriver
}

// DigestCache is a set of metrics for the digest cache subsystem.
type DigestCache interface {
	DigestCache() Cache
	DigestCacheScoped() Cache
}

func dockerErrorCode(err error) string {
	if e, ok := err.(errcode.Error); ok {
		return e.ErrorCode().String()
	}
	return "UNKNOWN"
}

func pullthroughRepositoryWrapper(ctx context.Context, sink Sink, registry string, funcname string, f func(ctx context.Context) error) error {
	registry = strings.ToLower(registry)
	defer NewTimer(sink.PullthroughRepositoryDuration(registry, funcname)).Stop()
	err := f(ctx)
	if err != nil {
		sink.PullthroughRepositoryErrors(registry, funcname, dockerErrorCode(err)).Inc()
	}
	return err
}

type repositoryRetriever struct {
	retriever registryclient.RepositoryRetriever
	sink      Sink
}

func (rr repositoryRetriever) Repository(ctx context.Context, registry *url.URL, repoName string, insecure bool) (repo distribution.Repository, err error) {
	err = pullthroughRepositoryWrapper(ctx, rr.sink, registry.Host, "Init", func(ctx context.Context) error {
		repo, err = rr.retriever.Repository(ctx, registry, repoName, insecure)
		return err
	})
	if err != nil {
		return repo, err
	}
	return wrapped.NewRepository(repo, func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		return pullthroughRepositoryWrapper(ctx, rr.sink, registry.Host, funcname, f)
	}), nil
}

func storageSentinelError(err error) bool {
	if err == io.EOF {
		return true
	}
	if _, ok := err.(storagedriver.ErrUnsupportedMethod); ok {
		return true
	}
	return false
}

func storageErrorCode(err error) string {
	switch err.(type) {
	case storagedriver.ErrUnsupportedMethod:
		return "UNSUPPORTED_METHOD"
	case storagedriver.PathNotFoundError:
		return "PATH_NOT_FOUND"
	case storagedriver.InvalidPathError:
		return "INVALID_PATH"
	case storagedriver.InvalidOffsetError:
		return "INVALID_OFFSET"
	}
	return "UNKNOWN"
}

type metrics struct {
	sink Sink
}

var _ Metrics = &metrics{}

// NewMetrics returns a helper that exposes the metrics through sink to
// instrument the application.
func NewMetrics(sink Sink) Metrics {
	return &metrics{
		sink: sink,
	}
}

func (m *metrics) Repository(r distribution.Repository, reponame string) distribution.Repository {
	return wrapped.NewRepository(r, func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		defer NewTimer(m.sink.RequestDuration(funcname, reponame)).Stop()
		return f(ctx)
	})
}

func (m *metrics) RepositoryRetriever(retriever registryclient.RepositoryRetriever) registryclient.RepositoryRetriever {
	return repositoryRetriever{
		retriever: retriever,
		sink:      m.sink,
	}
}

func (m *metrics) DigestBlobStoreCache() Cache {
	return &cache{
		hitCounter:  m.sink.PullthroughBlobstoreCacheRequests("Hit"),
		missCounter: m.sink.PullthroughBlobstoreCacheRequests("Miss"),
	}
}

func (m *metrics) StorageDriver(driver storagedriver.StorageDriver) storagedriver.StorageDriver {
	return wrapped.NewStorageDriver(driver, func(funcname string, f func() error) error {
		defer NewTimer(m.sink.StorageDuration(funcname)).Stop()
		err := f()
		if err != nil && !storageSentinelError(err) {
			m.sink.StorageErrors(funcname, storageErrorCode(err)).Inc()
		}
		return err
	})
}

func (m *metrics) DigestCache() Cache {
	return &cache{
		hitCounter:  m.sink.DigestCacheRequests("Hit"),
		missCounter: m.sink.DigestCacheRequests("Miss"),
	}
}

func (m *metrics) DigestCacheScoped() Cache {
	return &cache{
		hitCounter:  m.sink.DigestCacheScopedRequests("Hit"),
		missCounter: m.sink.DigestCacheScopedRequests("Miss"),
	}
}

type noopMetrics struct {
}

var _ Metrics = noopMetrics{}

// NewNoopMetrics return a helper that implements the Metrics interface, but
// does nothing.
func NewNoopMetrics() Metrics {
	return noopMetrics{}
}

func (m noopMetrics) Repository(r distribution.Repository, reponame string) distribution.Repository {
	return r
}

func (m noopMetrics) RepositoryRetriever(retriever registryclient.RepositoryRetriever) registryclient.RepositoryRetriever {
	return retriever
}

func (m noopMetrics) DigestBlobStoreCache() Cache {
	return noopCache{}
}

func (m noopMetrics) StorageDriver(driver storagedriver.StorageDriver) storagedriver.StorageDriver {
	return driver
}

func (m noopMetrics) DigestCache() Cache {
	return noopCache{}
}

func (m noopMetrics) DigestCacheScoped() Cache {
	return noopCache{}
}
