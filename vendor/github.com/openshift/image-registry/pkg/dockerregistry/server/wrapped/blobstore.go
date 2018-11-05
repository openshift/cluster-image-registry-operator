package wrapped

import (
	"context"
	"net/http"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

// blobStore wraps a distribution.BlobStore.
type blobStore struct {
	blobStore distribution.BlobStore
	wrapper   Wrapper
}

var _ distribution.BlobStore = &blobStore{}

// NewBlobStore returns a wrapped distribution.BlobStore.
func NewBlobStore(bs distribution.BlobStore, wrapper Wrapper) distribution.BlobStore {
	return &blobStore{
		blobStore: bs,
		wrapper:   wrapper,
	}
}

func (bs *blobStore) Stat(ctx context.Context, dgst digest.Digest) (desc distribution.Descriptor, err error) {
	err = bs.wrapper(ctx, "BlobStore.Stat", func(ctx context.Context) error {
		desc, err = bs.blobStore.Stat(ctx, dgst)
		return err
	})
	return
}

func (bs *blobStore) Get(ctx context.Context, dgst digest.Digest) (p []byte, err error) {
	err = bs.wrapper(ctx, "BlobStore.Get", func(ctx context.Context) error {
		p, err = bs.blobStore.Get(ctx, dgst)
		return err
	})
	return
}

func (bs *blobStore) Open(ctx context.Context, dgst digest.Digest) (r distribution.ReadSeekCloser, err error) {
	err = bs.wrapper(ctx, "BlobStore.Open", func(ctx context.Context) error {
		r, err = bs.blobStore.Open(ctx, dgst)
		return err
	})
	return
}

func (bs *blobStore) Put(ctx context.Context, mediaType string, p []byte) (desc distribution.Descriptor, err error) {
	err = bs.wrapper(ctx, "BlobStore.Put", func(ctx context.Context) error {
		desc, err = bs.blobStore.Put(ctx, mediaType, p)
		return err
	})
	return
}

func (bs *blobStore) Create(ctx context.Context, options ...distribution.BlobCreateOption) (w distribution.BlobWriter, err error) {
	err = bs.wrapper(ctx, "BlobStore.Create", func(ctx context.Context) error {
		w, err = bs.blobStore.Create(ctx, options...)
		return err
	})
	if err == nil {
		w = &blobWriter{
			BlobWriter: w,
			wrapper:    bs.wrapper,
		}
	}
	return
}

func (bs *blobStore) Resume(ctx context.Context, id string) (w distribution.BlobWriter, err error) {
	err = bs.wrapper(ctx, "BlobStore.Resume", func(ctx context.Context) error {
		w, err = bs.blobStore.Resume(ctx, id)
		return err
	})
	if err == nil {
		w = &blobWriter{
			BlobWriter: w,
			wrapper:    bs.wrapper,
		}
	}
	return
}

func (bs *blobStore) ServeBlob(ctx context.Context, w http.ResponseWriter, req *http.Request, dgst digest.Digest) error {
	return bs.wrapper(ctx, "BlobStore.ServeBlob", func(ctx context.Context) error {
		return bs.blobStore.ServeBlob(ctx, w, req, dgst)
	})
}

func (bs *blobStore) Delete(ctx context.Context, dgst digest.Digest) error {
	return bs.wrapper(ctx, "BlobStore.Delete", func(ctx context.Context) error {
		return bs.blobStore.Delete(ctx, dgst)
	})
}
