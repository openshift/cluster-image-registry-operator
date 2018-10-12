package cache

import (
	"context"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

type RepositoryScopedBlobDescriptor struct {
	Repo  string
	Cache DigestCache
	Svc   distribution.BlobDescriptorService
}

var _ distribution.BlobDescriptorService = &RepositoryScopedBlobDescriptor{}

// Stat provides metadata about a blob identified by the digest.
func (rbd *RepositoryScopedBlobDescriptor) Stat(ctx context.Context, dgst digest.Digest) (distribution.Descriptor, error) {
	desc, err := rbd.Cache.ScopedGet(dgst, rbd.Repo)
	if err == nil || err != distribution.ErrBlobUnknown || rbd.Svc == nil {
		return desc, err
	}

	desc, err = rbd.Svc.Stat(ctx, dgst)
	if err != nil {
		return desc, err
	}

	_ = rbd.Cache.Add(dgst, &DigestValue{
		repo: &rbd.Repo,
		desc: &desc,
	})

	return desc, nil
}

// Clear removes digest from the repository cache
func (rbd *RepositoryScopedBlobDescriptor) Clear(ctx context.Context, dgst digest.Digest) error {
	err := rbd.Cache.ScopedRemove(dgst, rbd.Repo)
	if err != nil {
		return err
	}
	if rbd.Svc != nil {
		return rbd.Svc.Clear(ctx, dgst)
	}
	return nil
}

// SetDescriptor assigns the descriptor to the digest for repository
func (rbd *RepositoryScopedBlobDescriptor) SetDescriptor(ctx context.Context, dgst digest.Digest, desc distribution.Descriptor) error {
	err := rbd.Cache.Add(dgst, &DigestValue{
		desc: &desc,
		repo: &rbd.Repo,
	})
	if err != nil {
		return err
	}
	if rbd.Svc != nil {
		return rbd.Svc.SetDescriptor(ctx, dgst, desc)
	}
	return nil
}
