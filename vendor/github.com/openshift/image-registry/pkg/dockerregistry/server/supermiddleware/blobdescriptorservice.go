package supermiddleware

import (
	"context"

	"github.com/sirupsen/logrus"

	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
	registrymw "github.com/docker/distribution/registry/middleware/registry"
	"github.com/docker/distribution/registry/storage"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/wrapped"
)

type blobDescriptorServiceFactoryFunc func(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService

var _ distribution.BlobDescriptorServiceFactory = blobDescriptorServiceFactoryFunc(nil)

func (f blobDescriptorServiceFactoryFunc) BlobAccessController(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService {
	return f(svc)
}

type blobDescriptorServiceFactoryContextKey struct{}

func withBlobDescriptorServiceFactory(ctx context.Context, f distribution.BlobDescriptorServiceFactory) context.Context {
	return context.WithValue(ctx, blobDescriptorServiceFactoryContextKey{}, f)
}

func blobDescriptorServiceFactoryFrom(ctx context.Context) distribution.BlobDescriptorServiceFactory {
	f, _ := ctx.Value(blobDescriptorServiceFactoryContextKey{}).(distribution.BlobDescriptorServiceFactory)
	return f
}

type blobDescriptorServiceFactory struct{}

var _ distribution.BlobDescriptorServiceFactory = &blobDescriptorServiceFactory{}

func (f *blobDescriptorServiceFactory) BlobAccessController(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService {
	return &blobDescriptorService{upstream: svc}
}

type blobDescriptorService struct {
	upstream distribution.BlobDescriptorService
	impl     distribution.BlobDescriptorService
}

var _ distribution.BlobDescriptorService = &blobDescriptorService{}

func (bds *blobDescriptorService) getImpl(ctx context.Context) distribution.BlobDescriptorService {
	if bds.impl == nil {
		bds.impl = bds.upstream
		if factory := blobDescriptorServiceFactoryFrom(ctx); factory != nil {
			bds.impl = factory.BlobAccessController(bds.impl)
		}
	}
	return bds.impl
}

func (bds *blobDescriptorService) Stat(ctx context.Context, dgst digest.Digest) (distribution.Descriptor, error) {
	return bds.getImpl(ctx).Stat(ctx, dgst)
}

func (bds *blobDescriptorService) SetDescriptor(ctx context.Context, dgst digest.Digest, desc distribution.Descriptor) error {
	return bds.getImpl(ctx).SetDescriptor(ctx, dgst, desc)
}

func (bds *blobDescriptorService) Clear(ctx context.Context, dgst digest.Digest) error {
	return bds.getImpl(ctx).Clear(ctx, dgst)
}

func init() {
	err := registrymw.RegisterOptions(storage.BlobDescriptorServiceFactory(&blobDescriptorServiceFactory{}))
	if err != nil {
		logrus.Fatalf("Unable to register BlobDescriptorServiceFactory: %v", err)
	}
}

func newBlobDescriptorServiceRepository(repo distribution.Repository, factory distribution.BlobDescriptorServiceFactory) distribution.Repository {
	return wrapped.NewRepository(repo, func(ctx context.Context, funcname string, f func(ctx context.Context) error) error {
		return f(withBlobDescriptorServiceFactory(ctx, factory))
	})
}

// effectiveCreateOptions find out what the blob creation options are going to do by dry-running them.
func effectiveCreateOptions(options []distribution.BlobCreateOption) (*distribution.CreateOptions, error) {
	opts := &distribution.CreateOptions{}
	for _, createOptions := range options {
		if err := createOptions.Apply(opts); err != nil {
			return nil, err
		}
	}
	return opts, nil
}

type blobDescriptorServiceBlobStore struct {
	distribution.BlobStore
	inst *instance
}

func (bs blobDescriptorServiceBlobStore) Create(ctx context.Context, options ...distribution.BlobCreateOption) (distribution.BlobWriter, error) {
	opts, err := effectiveCreateOptions(options)
	if err != nil {
		return nil, err
	}

	if opts.Mount.ShouldMount {
		named, err := reference.WithName(opts.Mount.From.Name())
		if err != nil {
			return nil, err
		}
		sourceRepo, err := bs.inst.registry.Repository(ctx, named)
		if err != nil {
			return nil, err
		}
		_, bdsf, err := bs.inst.App.Repository(ctx, sourceRepo, true)
		if err != nil {
			return nil, err
		}
		ctx = withBlobDescriptorServiceFactory(ctx, bdsf)
	}

	return bs.BlobStore.Create(ctx, options...)
}

type blobDescriptorServiceRepository struct {
	distribution.Repository
	inst *instance
}

func (r blobDescriptorServiceRepository) Blobs(ctx context.Context) distribution.BlobStore {
	return blobDescriptorServiceBlobStore{
		BlobStore: r.Repository.Blobs(ctx),
		inst:      r.inst,
	}
}
