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

func TestRepositoryScopedBlobDescriptor(t *testing.T) {
	repo := "foo"
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	cache.(*digestCache).clock = clock

	svc := &fakeBlobDescriptorService{
		digests: map[digest.Digest]distribution.Descriptor{
			dgst: {
				Digest: dgst,
				Size:   2,
			},
		},
	}

	bs := &RepositoryScopedBlobDescriptor{
		Repo:  repo,
		Cache: cache,
		Svc:   svc,
	}

	desc, err := bs.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected absence of a request to svc")
	}

	desc, err = bs.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected number of requests to svc (expected 1, got %d)", svc.counter["Stat"][dgst])
	}

	clock.Step(ttl5m)

	desc, err = bs.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if svc.counter["Stat"][dgst] != 2 {
		t.Fatalf("unexpected number of requests to svc (expected 2, got %d)", svc.counter["Stat"][dgst])
	}
}

func TestRepositoryScopedBlobDescriptorFail(t *testing.T) {
	repo := "foo"
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	cache.(*digestCache).clock = clock

	svc := &fakeBlobDescriptorService{}

	bs := &RepositoryScopedBlobDescriptor{
		Repo:  repo,
		Cache: cache,
		Svc:   svc,
	}

	_, err = bs.Stat(context.Background(), dgst)

	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRepositoryScopedBlobDescriptorClear(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	svc := &fakeBlobDescriptorService{
		digests: map[digest.Digest]distribution.Descriptor{
			dgst: {
				Digest: dgst,
				Size:   2,
			},
		},
	}

	bs := &RepositoryScopedBlobDescriptor{
		Cache: cache,
		Svc:   svc,
	}

	_, err = bs.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	_, err = cache.Get(dgst)
	if err != nil {
		t.Fatal(err)
	}

	err = bs.Clear(context.Background(), dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cache.Get(dgst)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := svc.digests[dgst]; ok {
		t.Fatalf("digest is not removed")
	}

	_, err = bs.Stat(context.Background(), dgst)

	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = cache.Get(dgst)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryScopedBlobDescriptorAdd(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	svc := &fakeBlobDescriptorService{}

	bs := &RepositoryScopedBlobDescriptor{
		Cache: cache,
		Svc:   svc,
	}

	desc, err := bs.Stat(context.Background(), dgst)
	if err == nil {
		t.Fatalf("unexpected result: %#+v", desc)
	}

	err = bs.SetDescriptor(context.Background(), dgst, distribution.Descriptor{
		Digest: dgst,
		Size:   2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := svc.digests[dgst]; !ok {
		t.Fatalf("digest not found")
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected number of Stat requests to svc (expected 1, got %d)", svc.counter["Stat"][dgst])
	}

	if svc.counter["SetDescriptor"][dgst] != 1 {
		t.Fatalf("unexpected number of SetDescriptor requests to svc (expected 1, got %d)", svc.counter["SetDescriptor"][dgst])
	}

	_, err = bs.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected number of Stat requests to svc (expected 1, got %d)", svc.counter["Stat"][dgst])
	}
}
