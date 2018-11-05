package cache

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/metrics"
)

const (
	ttl1m = time.Minute
	ttl5m = time.Minute * 5
)

func TestDigestCacheAddDigest(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")
	repo := "foo"
	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	cache.(*digestCache).clock = clock

	cache.Add(dgst, &DigestValue{
		desc: &distribution.Descriptor{
			Digest: dgst,
			Size:   1234,
		},
		repo: &repo,
	})

	desc, err := cache.ScopedGet(dgst, repo)
	if err != nil {
		t.Fatal(err)
	}

	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	clock.Step(ttl5m)

	_, err = cache.Get(dgst)
	if err == nil || err != distribution.ErrBlobUnknown {
		t.Fatalf("item not expired")
	}

	return
}

func TestDigestCacheRemoveDigest(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")
	desc := distribution.Descriptor{
		Size:   10,
		Digest: dgst,
	}

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	err = cache.Add(dgst, &DigestValue{
		desc: &desc,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = cache.Get(dgst)
	if err != nil {
		t.Fatal(err)
	}

	err = cache.Remove(dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return
}

func TestDigestCacheAddRepository(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")
	repos := []string{"foo", "bar", "baz"}

	cache, err := NewBlobDigest(5, 1, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range repos {
		cache.Add(dgst, &DigestValue{
			repo: &v,
		})
	}

	dgstRepos := cache.Repositories(dgst)
	if len(dgstRepos) != 1 || dgstRepos[0] != repos[len(repos)-1] {
		t.Fatalf("got %q, want [%s]", dgstRepos, repos[len(repos)-1])
	}
}

func TestDigestCacheScopedRemove(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")
	repos := []string{"bar", "baz", "foo"}

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range repos {
		cache.Add(dgst, &DigestValue{
			repo: &v,
		})
	}

	dgstRepos := cache.Repositories(dgst)
	sort.Strings(dgstRepos)
	if !reflect.DeepEqual(dgstRepos, repos) {
		t.Fatalf("got %q, want %q", dgstRepos, repos)
	}

	for i, v := range repos {
		err = cache.ScopedRemove(dgst, v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		dgstRepos := cache.Repositories(dgst)
		if dgstRepos == nil {
			dgstRepos = []string{}
		}
		sort.Strings(dgstRepos)
		if !reflect.DeepEqual(dgstRepos, repos[i+1:]) {
			t.Fatalf("got %q, want %q", dgstRepos, repos[i+1:])
		}
	}
}

func TestDigestCacheInvalidDigest(t *testing.T) {
	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	_, err = cache.Get(digest.Digest("XXX"))
	if err != digest.ErrDigestInvalidFormat {
		t.Fatalf("unexpected answer: %v", err)
	}

	err = cache.Add(digest.Digest("XXX"), &DigestValue{})
	if err != digest.ErrDigestInvalidFormat {
		t.Fatalf("unexpected answer: %v", err)
	}

	err = cache.Remove(digest.Digest("XXX"))
	if err != digest.ErrDigestInvalidFormat {
		t.Fatalf("unexpected answer: %v", err)
	}

	err = cache.ScopedRemove(digest.Digest("XXX"), "foo")
	if err != digest.ErrDigestInvalidFormat {
		t.Fatalf("unexpected answer: %v", err)
	}
}

func TestDigestCacheDigestMigration(t *testing.T) {
	dgst256 := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")
	dgst512 := digest.Digest("sha512:3abb6677af34ac57c0ca5828fd94f9d886c26ce59a8ce60ecf6778079423dccff1d6f19cb655805d56098e6d38a1a710dee59523eed7511e5a9e4b8ccb3a4686")

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	cache.Add(dgst256, &DigestValue{
		desc: &distribution.Descriptor{
			Digest: dgst512,
			Size:   1234,
		},
	})

	desc256, err := cache.Get(dgst256)
	if err != nil {
		t.Fatal(err)
	}

	desc512, err := cache.Get(dgst512)
	if err != nil {
		t.Fatal(err)
	}

	if desc256.Digest != desc512.Digest {
		t.Fatalf("unexpected digest: %#+v != %#+v", desc256.Digest, desc512.Digest)
	}
}
