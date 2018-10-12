package wrapped

import (
	"context"

	"github.com/docker/distribution"
)

// blobWriter wraps a distribution.BlobWriter.
type blobWriter struct {
	distribution.BlobWriter
	wrapper Wrapper
}

// NewBlobWriter returns a wrapped distribution.BlobWriter.
func NewBlobWriter(bw distribution.BlobWriter, wrapper Wrapper) distribution.BlobWriter {
	return &blobWriter{
		BlobWriter: bw,
		wrapper:    wrapper,
	}
}

func (bw *blobWriter) Commit(ctx context.Context, provisional distribution.Descriptor) (canonical distribution.Descriptor, err error) {
	err = bw.wrapper(ctx, "BlobWriter.Commit", func(ctx context.Context) error {
		canonical, err = bw.BlobWriter.Commit(ctx, provisional)
		return err
	})
	return
}

func (bw *blobWriter) Cancel(ctx context.Context) error {
	return bw.wrapper(ctx, "BlobWriter.Cancel", func(ctx context.Context) error {
		return bw.BlobWriter.Cancel(ctx)
	})
}
