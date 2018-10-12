package server

import (
	"context"

	"github.com/docker/distribution"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/wrapped"
)

// newPendingErrorsWrapper ensures auth completed and there were no errors relevant to the repo.
func newPendingErrorsWrapper(repo *repository) wrapped.Wrapper {
	return func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		if err := repo.checkPendingErrors(ctx); err != nil {
			return err
		}
		return f(ctx)
	}
}

func newPendingErrorsBlobStore(bs distribution.BlobStore, repo *repository) distribution.BlobStore {
	return wrapped.NewBlobStore(bs, newPendingErrorsWrapper(repo))
}

func newPendingErrorsManifestService(ms distribution.ManifestService, repo *repository) distribution.ManifestService {
	return wrapped.NewManifestService(ms, newPendingErrorsWrapper(repo))
}

func newPendingErrorsTagService(ts distribution.TagService, repo *repository) distribution.TagService {
	return wrapped.NewTagService(ts, newPendingErrorsWrapper(repo))
}

func newPendingErrorsBlobDescriptorService(bds distribution.BlobDescriptorService, repo *repository) distribution.BlobDescriptorService {
	return wrapped.NewBlobDescriptorService(bds, newPendingErrorsWrapper(repo))
}
