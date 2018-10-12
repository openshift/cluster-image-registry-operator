package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	registrystorage "github.com/docker/distribution/registry/storage"

	restclient "k8s.io/client-go/rest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/audit"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/cache"
	"github.com/openshift/image-registry/pkg/imagestream"
)

var (
	// secureTransport is the transport pool used for pullthrough to remote registries marked as
	// secure.
	secureTransport http.RoundTripper
	// insecureTransport is the transport pool that does not verify remote TLS certificates for use
	// during pullthrough against registries marked as insecure.
	insecureTransport http.RoundTripper
)

func init() {
	secureTransport = http.DefaultTransport
	var err error
	insecureTransport, err = restclient.TransportFor(&restclient.Config{TLSClientConfig: restclient.TLSClientConfig{Insecure: true}})
	if err != nil {
		panic(fmt.Sprintf("Unable to configure a default transport for importing insecure images: %v", err))
	}
}

// repository wraps a distribution.Repository and allows manifests to be served from the OpenShift image
// API.
type repository struct {
	distribution.Repository

	ctx        context.Context
	app        *App
	crossmount bool

	imageStream imagestream.ImageStream

	// remoteBlobGetter is used to fetch blobs from remote registries if pullthrough is enabled.
	remoteBlobGetter BlobGetterService
	cache            cache.RepositoryDigest
}

// Repository returns a new repository middleware.
func (app *App) Repository(ctx context.Context, repo distribution.Repository, crossmount bool) (distribution.Repository, distribution.BlobDescriptorServiceFactory, error) {
	registryOSClient, err := app.registryClient.Client()
	if err != nil {
		return nil, nil, err
	}

	namespace, name, err := getNamespaceName(repo.Named().Name())
	if err != nil {
		return nil, nil, err
	}

	r := &repository{
		Repository: repo,

		ctx:        ctx,
		app:        app,
		crossmount: crossmount,

		imageStream: imagestream.New(ctx, namespace, name, registryOSClient),
		cache:       cache.NewRepositoryDigest(app.cache),
	}

	r.remoteBlobGetter = NewBlobGetterService(
		r.imageStream,
		r.imageStream.GetSecrets,
		r.cache,
		r.app.metrics,
	)

	repo = distribution.Repository(r)
	repo = r.app.metrics.Repository(repo, repo.Named().Name())

	bdsf := blobDescriptorServiceFactoryFunc(r.BlobDescriptorService)

	return repo, bdsf, nil
}

// Manifests returns r, which implements distribution.ManifestService.
func (r *repository) Manifests(ctx context.Context, options ...distribution.ManifestServiceOption) (distribution.ManifestService, error) {
	// We do a verification of our own. We do more restrictive checks and we
	// know about remote blobs.
	opts := append(options, registrystorage.SkipLayerVerification())
	ms, err := r.Repository.Manifests(ctx, opts...)
	if err != nil {
		return nil, err
	}

	ms = &manifestService{
		manifests:     ms,
		blobStore:     r.Blobs(ctx),
		serverAddr:    r.app.config.Server.Addr,
		imageStream:   r.imageStream,
		cache:         r.cache,
		acceptSchema2: r.app.config.Compatibility.AcceptSchema2,
	}

	ms = &pullthroughManifestService{
		ManifestService: ms,
		newLocalManifestService: func(ctx context.Context) (distribution.ManifestService, error) {
			return r.Repository.Manifests(ctx, opts...)
		},
		imageStream:  r.imageStream,
		cache:        r.cache,
		mirror:       r.app.config.Pullthrough.Mirror,
		registryAddr: r.app.config.Server.Addr,
		metrics:      r.app.metrics,
	}

	ms = newPendingErrorsManifestService(ms, r)

	if audit.LoggerExists(ctx) {
		ms = audit.NewManifestService(ctx, ms)
	}

	return ms, nil
}

// Blobs returns a blob store which can delegate to remote repositories.
func (r *repository) Blobs(ctx context.Context) distribution.BlobStore {
	bs := r.Repository.Blobs(ctx)

	if r.app.quotaEnforcing.enforcementEnabled {
		bs = &quotaRestrictedBlobStore{
			BlobStore: bs,

			repo: r,
		}
	}

	bs = &pullthroughBlobStore{
		BlobStore: bs,

		remoteBlobGetter:  r.remoteBlobGetter,
		writeLimiter:      r.app.writeLimiter,
		mirror:            r.app.config.Pullthrough.Mirror,
		newLocalBlobStore: r.Repository.Blobs,
	}

	bs = newPendingErrorsBlobStore(bs, r)

	if audit.LoggerExists(ctx) {
		bs = audit.NewBlobStore(ctx, bs)
	}

	return bs
}

// Tags returns a reference to this repository tag service.
func (r *repository) Tags(ctx context.Context) distribution.TagService {
	ts := r.Repository.Tags(ctx)

	ts = &tagService{
		TagService:  ts,
		imageStream: r.imageStream,
	}

	ts = newPendingErrorsTagService(ts, r)

	if audit.LoggerExists(ctx) {
		ts = audit.NewTagService(ctx, ts)
	}

	return ts
}

func (r *repository) BlobDescriptorService(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService {
	svc = &cache.RepositoryScopedBlobDescriptor{
		Repo:  r.Named().String(),
		Cache: r.app.cache,
		Svc:   svc,
	}
	svc = &blobDescriptorService{svc, r}
	svc = newPendingErrorsBlobDescriptorService(svc, r)
	return svc
}

func (r *repository) checkPendingErrors(ctx context.Context) error {
	if !authPerformed(ctx) {
		return fmt.Errorf("openshift.auth.completed missing from context")
	}

	deferredErrors, haveDeferredErrors := deferredErrorsFrom(ctx)
	if !haveDeferredErrors {
		return nil
	}

	repoErr, haveRepoErr := deferredErrors.Get(r.imageStream.Reference())
	if !haveRepoErr {
		return nil
	}

	dcontext.GetLogger(r.ctx).Debugf("Origin auth: found deferred error for %s: %v", r.imageStream.Reference(), repoErr)

	return repoErr
}
