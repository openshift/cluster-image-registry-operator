package server

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/cache"
	"github.com/openshift/image-registry/pkg/dockerregistry/server/metrics"
	"github.com/openshift/image-registry/pkg/errors"
	"github.com/openshift/image-registry/pkg/imagestream"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
)

// pullthroughManifestService wraps a distribution.ManifestService
// repositories. Since the manifest is no longer stored in the Image
// the docker-registry must pull through requests to manifests as well
// as to blobs.
type pullthroughManifestService struct {
	distribution.ManifestService
	newLocalManifestService func(ctx context.Context) (distribution.ManifestService, error)
	imageStream             imagestream.ImageStream
	cache                   cache.RepositoryDigest
	mirror                  bool
	registryAddr            string
	metrics                 metrics.Pullthrough
}

var _ distribution.ManifestService = &pullthroughManifestService{}

func (m *pullthroughManifestService) Get(ctx context.Context, dgst digest.Digest, options ...distribution.ManifestServiceOption) (distribution.Manifest, error) {
	dcontext.GetLogger(ctx).Debugf("(*pullthroughManifestService).Get: starting with dgst=%s", dgst.String())

	manifest, err := m.ManifestService.Get(ctx, dgst, options...)
	if _, ok := err.(distribution.ErrManifestUnknownRevision); ok {
		return m.remoteGet(ctx, dgst, options...)
	}

	return manifest, err
}

func (m *pullthroughManifestService) remoteGet(ctx context.Context, dgst digest.Digest, options ...distribution.ManifestServiceOption) (distribution.Manifest, error) {
	dcontext.GetLogger(ctx).Debugf("(*pullthroughManifestService).remoteGet: starting with dgst=%s", dgst.String())
	image, rErr := m.imageStream.GetImageOfImageStream(ctx, dgst)
	if rErr != nil {
		switch rErr.Code {
		case imagestream.ErrImageStreamNotFoundCode, imagestream.ErrImageStreamImageNotFoundCode:
			dcontext.GetLogger(ctx).Errorf("remoteGet: unable to get image %s in imagestream %s: %v", dgst.String(), m.imageStream.Reference(), rErr)
			return nil, distribution.ErrManifestUnknownRevision{
				Name:     m.imageStream.Reference(),
				Revision: dgst,
			}
		case imagestream.ErrImageStreamForbiddenCode:
			dcontext.GetLogger(ctx).Errorf("remoteGet: unable to get access to imagestream %s to find image %s: %v", m.imageStream.Reference(), dgst.String(), rErr)
			return nil, distribution.ErrAccessDenied
		}
		return nil, rErr
	}

	ref, err := imageapi.ParseDockerImageReference(image.DockerImageReference)
	if err != nil {
		dcontext.GetLogger(ctx).Errorf("bad DockerImageReference (%q) in Image %s@%s: %v", image.DockerImageReference, m.imageStream.Reference(), dgst.String(), err)
		return nil, err
	}
	ref = ref.DockerClientDefaults()

	// don't attempt to pullthrough from ourself
	if ref.Registry == m.registryAddr {
		return nil, distribution.ErrManifestUnknownRevision{
			Name:     m.imageStream.Reference(),
			Revision: dgst,
		}
	}

	repo, err := m.getRemoteRepositoryClient(ctx, &ref, dgst, options...)
	if err != nil {
		return nil, errors.ErrorCodePullthroughManifest.WithArgs(ref.Exact(), err)
	}

	pullthroughManifestService, err := repo.Manifests(ctx)
	if err != nil {
		return nil, err
	}

	manifest, err := pullthroughManifestService.Get(ctx, dgst)
	if err != nil {
		return nil, errors.ErrorCodePullthroughManifest.WithArgs(ref.Exact(), err)
	}

	if m.mirror {
		if mirrorErr := m.mirrorManifest(ctx, manifest); mirrorErr != nil {
			errors.Handle(ctx, fmt.Sprintf("failed to mirror manifest from %s", ref.Exact()), mirrorErr)
		}
	}

	RememberLayersOfImage(ctx, m.cache, image, ref.Exact())

	return manifest, nil
}

func (m *pullthroughManifestService) mirrorManifest(ctx context.Context, manifest distribution.Manifest) error {
	localManifestService, err := m.newLocalManifestService(ctx)
	if err != nil {
		return fmt.Errorf("failed to create local manifest service: %v", err)
	}

	_, err = localManifestService.Put(ctx, manifest)
	return err
}

func (m *pullthroughManifestService) getRemoteRepositoryClient(ctx context.Context, ref *imageapi.DockerImageReference, dgst digest.Digest, options ...distribution.ManifestServiceOption) (distribution.Repository, error) {
	retriever := getImportContext(ctx, m.imageStream.GetSecrets, m.metrics)

	// determine, whether to fall-back to insecure transport based on a specification of image's tag
	// if the client pulls by tag, use that
	tag := ""
	for _, option := range options {
		if opt, ok := option.(distribution.WithTagOption); ok {
			tag = opt.Tag
			break
		}
	}

	insecure, err := m.imageStream.TagIsInsecure(ctx, tag, dgst)
	if err != nil {
		return nil, err
	}

	return retriever.Repository(ctx, ref.RegistryURL(), ref.RepositoryName(), insecure)
}
