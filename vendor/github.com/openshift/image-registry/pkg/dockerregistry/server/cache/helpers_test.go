package cache

import (
	"context"
	"sync"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"
)

var _ distribution.BlobStatter = &fakeBlobDescriptorService{}
var _ distribution.BlobDescriptorService = &fakeBlobDescriptorService{}

type fakeBlobDescriptorService struct {
	mu      sync.Mutex
	digests map[digest.Digest]distribution.Descriptor
	counter map[string]map[digest.Digest]int
}

func (bs *fakeBlobDescriptorService) lazyInit() {
	if bs.digests == nil {
		bs.digests = make(map[digest.Digest]distribution.Descriptor)
	}

	if bs.counter == nil {
		bs.counter = make(map[string]map[digest.Digest]int)
	}

	for _, n := range []string{"Stat", "Clear", "SetDescriptor"} {
		if _, ok := bs.counter[n]; !ok {
			bs.counter[n] = make(map[digest.Digest]int)
		}
	}
}

func (bs *fakeBlobDescriptorService) Stat(ctx context.Context, dgst digest.Digest) (distribution.Descriptor, error) {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.lazyInit()

	if _, ok := bs.counter["Stat"][dgst]; !ok {
		bs.counter["Stat"][dgst] = 0
	}

	bs.counter["Stat"][dgst] += 1

	v, ok := bs.digests[dgst]
	if !ok {
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	return v, nil
}

func (bs *fakeBlobDescriptorService) Clear(ctx context.Context, dgst digest.Digest) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.lazyInit()

	bs.counter["Clear"][dgst] += 1
	delete(bs.digests, dgst)

	return nil
}

func (bs *fakeBlobDescriptorService) SetDescriptor(ctx context.Context, dgst digest.Digest, desc distribution.Descriptor) error {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	bs.lazyInit()

	bs.counter["SetDescriptor"][dgst] += 1
	bs.digests[dgst] = desc

	return nil
}
