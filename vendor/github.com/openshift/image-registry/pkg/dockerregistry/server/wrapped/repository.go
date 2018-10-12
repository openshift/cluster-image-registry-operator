package wrapped

import (
	"context"

	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
)

type repository struct {
	repository distribution.Repository
	wrapper    Wrapper
}

var _ distribution.Repository = &repository{}

// NewRepository returns a repository that creates wrapped services.
func NewRepository(r distribution.Repository, wrapper Wrapper) distribution.Repository {
	return &repository{
		repository: r,
		wrapper:    wrapper,
	}
}

func (r *repository) Named() reference.Named {
	return r.repository.Named()
}

func (r *repository) Blobs(ctx context.Context) distribution.BlobStore {
	bs := r.repository.Blobs(ctx)
	return NewBlobStore(bs, r.wrapper)
}

func (r *repository) Manifests(ctx context.Context, options ...distribution.ManifestServiceOption) (distribution.ManifestService, error) {
	ms, err := r.repository.Manifests(ctx, options...)
	if err != nil {
		return ms, err
	}
	return NewManifestService(ms, r.wrapper), nil
}

func (r *repository) Tags(ctx context.Context) distribution.TagService {
	ts := r.repository.Tags(ctx)
	return NewTagService(ts, r.wrapper)
}
