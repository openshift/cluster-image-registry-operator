package wrapped

import (
	"context"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

// blobDescriptorService wraps a distribution.BlobDescriptorService.
type blobDescriptorService struct {
	blobDescriptorService distribution.BlobDescriptorService
	wrapper               Wrapper
}

var _ distribution.BlobDescriptorService = &blobDescriptorService{}

// NewBlobDescriptorService NewBlobStore returns a wrapped distribution.BlobDescriptorService.
func NewBlobDescriptorService(bds distribution.BlobDescriptorService, wrapper Wrapper) distribution.BlobDescriptorService {
	return &blobDescriptorService{
		blobDescriptorService: bds,
		wrapper:               wrapper,
	}
}

func (bds *blobDescriptorService) Stat(ctx context.Context, dgst digest.Digest) (desc distribution.Descriptor, err error) {
	err = bds.wrapper(ctx, "BlobDescriptorService.Stat", func(ctx context.Context) error {
		desc, err = bds.blobDescriptorService.Stat(ctx, dgst)
		return err
	})
	return
}

func (bds *blobDescriptorService) Clear(ctx context.Context, dgst digest.Digest) error {
	return bds.wrapper(ctx, "BlobDescriptorService.Clear", func(ctx context.Context) error {
		return bds.blobDescriptorService.Clear(ctx, dgst)
	})
}

func (bds *blobDescriptorService) SetDescriptor(ctx context.Context, dgst digest.Digest, desc distribution.Descriptor) error {
	return bds.wrapper(ctx, "BlobDescriptorService.SetDescriptor", func(ctx context.Context) error {
		return bds.blobDescriptorService.SetDescriptor(ctx, dgst, desc)
	})
}
