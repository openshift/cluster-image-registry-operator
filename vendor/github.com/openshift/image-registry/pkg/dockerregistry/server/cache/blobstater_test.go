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

func TestBlobStatter(t *testing.T) {
	now := time.Now()
	clock := clock.NewFakeClock(now)

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	cache.(*digestCache).clock = clock

	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	svc := &fakeBlobDescriptorService{
		digests: map[digest.Digest]distribution.Descriptor{
			dgst: {
				Digest: dgst,
				Size:   2,
			},
		},
	}

	statter := &BlobStatter{
		Cache: cache,
		Svc:   svc,
	}

	desc, err := statter.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if desc.Digest != dgst {
		t.Fatalf("unexpected descriptor: %#+v", desc)
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected absence of a request to svc")
	}

	desc, err = statter.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if svc.counter["Stat"][dgst] != 1 {
		t.Fatalf("unexpected number of requests to svc (expected 1, got %d)", svc.counter["Stat"][dgst])
	}

	clock.Step(ttl5m)

	desc, err = statter.Stat(context.Background(), dgst)
	if err != nil {
		t.Fatal(err)
	}

	if svc.counter["Stat"][dgst] != 2 {
		t.Fatalf("unexpected number of requests to svc (expected 2, got %d)", svc.counter["Stat"][dgst])
	}
}

func TestBlobStatterFail(t *testing.T) {
	dgst := digest.Digest("sha256:4355a46b19d348dc2f57c046f8ef63d4538ebb936000f3c9ee954a27460dd865")

	cache, err := NewBlobDigest(5, 3, ttl1m, metrics.NewNoopMetrics())
	if err != nil {
		t.Fatal(err)
	}

	svc := &fakeBlobDescriptorService{}

	statter := &BlobStatter{
		Cache: cache,
		Svc:   svc,
	}

	_, err = statter.Stat(context.Background(), dgst)
	if err != distribution.ErrBlobUnknown {
		t.Fatalf("unexpected error: %v", err)
	}
}
