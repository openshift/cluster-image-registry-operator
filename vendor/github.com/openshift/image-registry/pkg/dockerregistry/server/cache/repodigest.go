package cache

import "github.com/opencontainers/go-digest"

type RepositoryDigest interface {
	AddDigest(dgst digest.Digest, repository string) error
	ContainsRepository(dgst digest.Digest, repository string) bool
	Repositories(dgst digest.Digest) []string
}

type repositoryDigest struct {
	Cache DigestCache
}

var _ RepositoryDigest = &repositoryDigest{}

func NewRepositoryDigest(cache DigestCache) RepositoryDigest {
	return &repositoryDigest{
		Cache: cache,
	}
}

func (rd *repositoryDigest) AddDigest(dgst digest.Digest, repository string) error {
	return rd.Cache.Add(dgst, &DigestValue{
		repo: &repository,
	})
}

func (rd *repositoryDigest) ContainsRepository(dgst digest.Digest, repository string) bool {
	for _, repo := range rd.Cache.Repositories(dgst) {
		if repo == repository {
			return true
		}
	}
	return false
}

func (rd *repositoryDigest) Repositories(dgst digest.Digest) []string {
	return rd.Cache.Repositories(dgst)
}
