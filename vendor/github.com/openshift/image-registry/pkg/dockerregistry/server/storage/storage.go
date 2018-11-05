package storage

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	"github.com/docker/distribution/reference"
	"github.com/opencontainers/go-digest"
)

type Enumerator struct {
	Registry distribution.Namespace
}

func (e *Enumerator) Repositories(ctx context.Context, handler func(string) error) error {
	repositoryEnumerator, ok := e.Registry.(distribution.RepositoryEnumerator)
	if !ok {
		return fmt.Errorf("unable to convert Namespace to RepositoryEnumerator")
	}
	return repositoryEnumerator.Enumerate(ctx, handler)
}

func (e *Enumerator) Blobs(ctx context.Context, handler func(digest.Digest) error) error {
	return e.Registry.Blobs().Enumerate(ctx, handler)
}

func (e *Enumerator) Manifests(ctx context.Context, repoName string, handler func(digest.Digest) error) error {
	named, err := reference.WithName(repoName)
	if err != nil {
		return fmt.Errorf("failed to parse the repo name %s: %v", repoName, err)
	}

	repository, err := e.Registry.Repository(ctx, named)
	if err != nil {
		return err
	}

	manifestService, err := repository.Manifests(ctx)
	if err != nil {
		return err
	}

	manifestEnumerator, ok := manifestService.(distribution.ManifestEnumerator)
	if !ok {
		return fmt.Errorf("unable to convert ManifestService into ManifestEnumerator")
	}

	return manifestEnumerator.Enumerate(ctx, handler)
}
