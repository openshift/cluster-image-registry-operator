package audit

import (
	"context"

	"github.com/docker/distribution"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/wrapped"
)

func newWrapper(ctx context.Context) wrapped.Wrapper {
	logger := GetLogger(ctx)
	return func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		logger.Log(funcname)
		err := f(ctx)
		logger.LogResult(err, funcname)
		return err
	}
}

// NewBlobStore wraps a distribution.BlobStore to track operation results and
// write them to the audit log.
func NewBlobStore(ctx context.Context, bs distribution.BlobStore) distribution.BlobStore {
	return wrapped.NewBlobStore(bs, newWrapper(ctx))
}

// NewManifestService wraps a distribution.ManifestService to track operation
// results and write them to the audit log.
func NewManifestService(ctx context.Context, ms distribution.ManifestService) distribution.ManifestService {
	return wrapped.NewManifestService(ms, newWrapper(ctx))
}

// NewTagService wraps a distribution.TagService to track operation results
// and write them to the audit log,
func NewTagService(ctx context.Context, ts distribution.TagService) distribution.TagService {
	return wrapped.NewTagService(ts, newWrapper(ctx))
}
