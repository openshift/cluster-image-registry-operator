package wrapped

import (
	"context"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

// manifestService wraps a distribution.ManifestService.
type manifestService struct {
	manifestService distribution.ManifestService
	wrapper         Wrapper
}

var _ distribution.ManifestService = &manifestService{}

// NewManifestService returns a wrapped distribution.ManifestService.
func NewManifestService(ms distribution.ManifestService, wrapper Wrapper) distribution.ManifestService {
	return &manifestService{
		manifestService: ms,
		wrapper:         wrapper,
	}
}

func (ms *manifestService) Exists(ctx context.Context, dgst digest.Digest) (ok bool, err error) {
	err = ms.wrapper(ctx, "ManifestService.Exists", func(ctx context.Context) error {
		ok, err = ms.manifestService.Exists(ctx, dgst)
		return err
	})
	return
}

func (ms *manifestService) Get(ctx context.Context, dgst digest.Digest, options ...distribution.ManifestServiceOption) (manifest distribution.Manifest, err error) {
	err = ms.wrapper(ctx, "ManifestService.Get", func(ctx context.Context) error {
		manifest, err = ms.manifestService.Get(ctx, dgst, options...)
		return err
	})
	return
}

func (ms *manifestService) Put(ctx context.Context, manifest distribution.Manifest, options ...distribution.ManifestServiceOption) (dgst digest.Digest, err error) {
	err = ms.wrapper(ctx, "ManifestService.Put", func(ctx context.Context) error {
		dgst, err = ms.manifestService.Put(ctx, manifest, options...)
		return err
	})
	return
}

func (ms *manifestService) Delete(ctx context.Context, dgst digest.Digest) error {
	return ms.wrapper(ctx, "ManifestService.Delete", func(ctx context.Context) error {
		return ms.manifestService.Delete(ctx, dgst)
	})
}
