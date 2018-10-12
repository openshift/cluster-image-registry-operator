package server

import (
	"context"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"
	"github.com/opencontainers/go-digest"
)

const (
	// DigestSha256EmptyTar is the canonical sha256 digest of empty data
	digestSha256EmptyTar = digest.Digest("sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")

	// digestSHA256GzippedEmptyTar is the canonical sha256 digest of gzippedEmptyTar
	digestSHA256GzippedEmptyTar = digest.Digest("sha256:a3ed95caeb02ffe68cdd9fd84406680ae93d633cb16422d00e8a7c22955b46d4")
)

func isEmptyDigest(dgst digest.Digest) bool {
	return dgst == digestSha256EmptyTar || dgst == digestSHA256GzippedEmptyTar
}

type blobDescriptorServiceFactoryFunc func(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService

func (f blobDescriptorServiceFactoryFunc) BlobAccessController(svc distribution.BlobDescriptorService) distribution.BlobDescriptorService {
	return f(svc)
}

type blobDescriptorService struct {
	distribution.BlobDescriptorService
	repo *repository
}

// Stat returns a a blob descriptor if the given blob is either linked in repository or is referenced in
// corresponding image stream. This method is invoked from inside of upstream's linkedBlobStore. It expects
// a proper repository object to be set on given context by upper openshift middleware wrappers.
func (bs *blobDescriptorService) Stat(ctx context.Context, dgst digest.Digest) (distribution.Descriptor, error) {
	dcontext.GetLogger(ctx).Debugf("(*blobDescriptorService).Stat: starting with digest=%s", dgst.String())

	// if there is a repo layer link, return its descriptor
	desc, err := bs.BlobDescriptorService.Stat(ctx, dgst)
	if err == nil {
		return desc, nil
	}
	dcontext.GetLogger(ctx).Debugf("(*blobDescriptorService).Stat: could not stat layer link %s in repository %s: %v", dgst.String(), bs.repo.Named().Name(), err)

	// we couldn't find the layer link
	desc, err = bs.repo.app.BlobStatter().Stat(ctx, dgst)
	if err != nil {
		return desc, err
	}
	dcontext.GetLogger(ctx).Debugf("(*blobDescriptorService).Stat: blob %s exists in the global blob store", dgst.String())

	// Empty layers are considered to be "public" and we don't need to check whether they are referenced - schema v2
	// has no empty layers.
	if isEmptyDigest(dgst) {
		return desc, nil
	}

	// ensure it's referenced inside of corresponding image stream
	if bs.repo.cache.ContainsRepository(dgst, bs.repo.imageStream.Reference()) {
		dcontext.GetLogger(ctx).Debugf("(*blobDescriptorService).Stat: found cached blob %q in repository %s", dgst.String(), bs.repo.imageStream.Reference())
		return desc, nil
	}

	found, layers, image := bs.repo.imageStream.HasBlob(ctx, dgst)
	if !found {
		dcontext.GetLogger(ctx).Debugf("(*blobDescriptorService).Stat: blob %s is neither empty nor referenced in image stream %s", dgst.String(), bs.repo.Named().Name())
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	if layers != nil {
		// remember all the layers of matching image
		RememberLayersOfImageStream(ctx, bs.repo.cache, layers, bs.repo.imageStream.Reference())
	}
	if image != nil {
		// remember all the layers of matching image
		RememberLayersOfImage(ctx, bs.repo.cache, image, bs.repo.imageStream.Reference())
	}
	return desc, nil
}
