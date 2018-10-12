package cache

import (
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/docker/distribution"
	"github.com/docker/distribution/context"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/metrics"
)

func TestGlobalProviderStat(t *testing.T) {
	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}
	cache.(*digestCache).clock = clock

	cacheprovider := &Provider{
		Cache: cache,
	}

	_, err = cacheprovider.Stat(context.Background(), digest.Digest("sha256:foo"))
	if err == nil {
		t.Fatal("error expected")
	}
	if err != digest.ErrDigestInvalidLength {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cacheprovider.Stat(context.Background(), digest.Digest("sha256:4355a46b19d348dc2f57c"))
	if err == nil {
		t.Fatal("error expected")
	}
	if err != digest.ErrDigestInvalidLength {
		t.Fatalf("unexpected error: %v", err)
	}

	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	desc, err := cacheprovider.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatal("error expected")
	}
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}

	err = cacheprovider.SetDescriptor(context.Background(), dgst, distribution.Descriptor{
		Digest: dgst,
		Size:   2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	desc, err = cacheprovider.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	clock.Step(ttl5m)

	desc, err = cacheprovider.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatal("error expected")
	}
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGlobalProviderClear(t *testing.T) {
	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}
	cache.(*digestCache).clock = clock

	cacheprovider := &Provider{
		Cache: cache,
	}

	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	err = cacheprovider.SetDescriptor(context.Background(), dgst, distribution.Descriptor{
		Digest: dgst,
		Size:   2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	desc, err := cacheprovider.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	err = cacheprovider.Clear(context.Background(), dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cacheprovider.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatal("error expected")
	}
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryScopedProviderStat(t *testing.T) {
	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}
	cache.(*digestCache).clock = clock

	cacheprovider := &Provider{
		Cache: cache,
	}

	repoprovider, err := cacheprovider.RepositoryScoped("foo|invalid|repository")
	if err == nil {
		t.Fatalf("error expected")
	}

	repoprovider, err = cacheprovider.RepositoryScoped("foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repoprovider.Stat(context.Background(), digest.Digest("sha256:foo"))
	if err == nil {
		t.Fatal("error expected")
	}
	if err != digest.ErrDigestInvalidLength {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cacheprovider.Stat(context.Background(), digest.Digest("sha256:4355a46b19d348dc2f57c"))
	if err == nil {
		t.Fatal("error expected")
	}
	if err != digest.ErrDigestInvalidLength {
		t.Fatalf("unexpected error: %v", err)
	}

	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	err = repoprovider.SetDescriptor(context.Background(), dgst, distribution.Descriptor{
		Digest: dgst,
		Size:   2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	desc, err := cacheprovider.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	desc, err = repoprovider.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	err = repoprovider.Clear(context.Background(), dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = repoprovider.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatal("error expected")
	}
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}

	desc, err = cacheprovider.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	clock.Step(ttl5m)

	_, err = cacheprovider.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatal("error expected")
	}
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}
