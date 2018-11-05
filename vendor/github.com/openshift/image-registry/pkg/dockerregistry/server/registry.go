package server

import (
	"context"
	"errors"

	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
)

// registry wraps upstream registry object and overrides some of its methods
// TODO: collect metrics for reimplemented methods
type registry struct {
	registry   distribution.Namespace
	enumerator RepositoryEnumerator
}

var _ distribution.Namespace = &registry{}

func (r *registry) Scope() distribution.Scope {
	return r.registry.Scope()
}

func (r *registry) Repository(ctx context.Context, name reference.Named) (distribution.Repository, error) {
	return r.registry.Repository(ctx, name)
}

// Repositories lists repository names made out of image streams fetched from master API.
func (r *registry) Repositories(ctx context.Context, repos []string, last string) (n int, err error) {
	n, err = r.enumerator.EnumerateRepositories(ctx, repos, last)
	if err == errNoSpaceInSlice {
		return n, errors.New("client requested zero entries")
	}
	return
}

func (r *registry) Blobs() distribution.BlobEnumerator {
	return r.registry.Blobs()
}

func (r *registry) BlobStatter() distribution.BlobStatter {
	return r.registry.BlobStatter()
}
