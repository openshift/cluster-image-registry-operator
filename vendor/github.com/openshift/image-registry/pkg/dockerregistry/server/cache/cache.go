package cache

import (
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/simplelru"
	"k8s.io/apimachinery/pkg/util/clock"

	"github.com/docker/distribution"
	"github.com/opencontainers/go-digest"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/metrics"
)

type DigestCache interface {
	Get(dgst digest.Digest) (distribution.Descriptor, error)
	ScopedGet(dgst digest.Digest, repository string) (distribution.Descriptor, error)
	Repositories(dgst digest.Digest) []string
	Remove(dgst digest.Digest) error
	ScopedRemove(dgst digest.Digest, repository string) error
	Add(dgst digest.Digest, value *DigestValue) error
}

type DigestValue struct {
	desc *distribution.Descriptor
	repo *string
}

type DigestItem struct {
	expireTime   time.Time
	desc         *distribution.Descriptor
	repositories *simplelru.LRU
}

type digestCache struct {
	ttl      time.Duration
	repoSize int
	metrics  metrics.DigestCache

	mu    sync.Mutex
	clock clock.Clock
	lru   *simplelru.LRU
}

func NewBlobDigest(digestSize, repoSize int, itemTTL time.Duration, metrics metrics.DigestCache) (DigestCache, error) {
	lru, err := simplelru.NewLRU(digestSize, nil)
	if err != nil {
		return nil, err
	}

	return &digestCache{
		ttl:      itemTTL,
		repoSize: repoSize,
		metrics:  metrics,
		clock:    clock.RealClock{},
		lru:      lru,
	}, nil
}

func (gbd *digestCache) get(dgst digest.Digest, reuse bool) *DigestItem {
	if value, ok := gbd.lru.Get(dgst); ok {
		d, _ := value.(*DigestItem)
		if d != nil && d.expireTime.Before(gbd.clock.Now()) {
			if !reuse {
				gbd.lru.Remove(dgst)
				return nil
			}
			d.expireTime = gbd.clock.Now().Add(gbd.ttl)
			d.desc = nil
			d.repositories.Purge()
		}
		return d
	}
	return nil
}

func (gbd *digestCache) peek(dgst digest.Digest) *DigestItem {
	if value, ok := gbd.lru.Peek(dgst); ok {
		d, _ := value.(*DigestItem)
		return d
	}
	return nil
}

func (gbd *digestCache) Get(dgst digest.Digest) (distribution.Descriptor, error) {
	if err := dgst.Validate(); err != nil {
		return distribution.Descriptor{}, err
	}

	if gbd.ttl == 0 {
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	value := gbd.get(dgst, false)

	if value == nil || value.desc == nil {
		gbd.metrics.DigestCache().Request(false)
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	gbd.metrics.DigestCache().Request(true)
	return *value.desc, nil
}

func (gbd *digestCache) ScopedGet(dgst digest.Digest, repository string) (distribution.Descriptor, error) {
	if err := dgst.Validate(); err != nil {
		return distribution.Descriptor{}, err
	}

	if gbd.ttl == 0 {
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	value := gbd.get(dgst, false)

	if value == nil || value.desc == nil || !value.repositories.Contains(repository) {
		gbd.metrics.DigestCacheScoped().Request(false)
		return distribution.Descriptor{}, distribution.ErrBlobUnknown
	}

	gbd.metrics.DigestCacheScoped().Request(true)
	return *value.desc, nil
}

func (gbd *digestCache) Repositories(dgst digest.Digest) []string {
	if err := dgst.Validate(); err != nil {
		return nil
	}

	if gbd.ttl == 0 {
		return nil
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	item := gbd.get(dgst, false)
	if item == nil {
		return nil
	}

	var repos []string
	for _, v := range item.repositories.Keys() {
		s := v.(string)
		repos = append(repos, s)
	}
	return repos
}

func (gbd *digestCache) Remove(dgst digest.Digest) error {
	if err := dgst.Validate(); err != nil {
		return err
	}

	if gbd.ttl == 0 {
		return nil
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	gbd.lru.Remove(dgst)
	return nil
}

func (gbd *digestCache) ScopedRemove(dgst digest.Digest, repository string) error {
	if err := dgst.Validate(); err != nil {
		return err
	}

	if gbd.ttl == 0 {
		return nil
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	value := gbd.peek(dgst)

	if value == nil {
		return nil
	}

	value.repositories.Remove(repository)
	return nil
}

func (gbd *digestCache) Add(dgst digest.Digest, item *DigestValue) error {
	if err := dgst.Validate(); err != nil {
		return err
	}

	if item == nil || (item.desc == nil && item.repo == nil) {
		return nil
	}

	if gbd.ttl == 0 {
		return nil
	}

	gbd.mu.Lock()
	defer gbd.mu.Unlock()

	value := gbd.get(dgst, true)

	if value == nil {
		lru, err := simplelru.NewLRU(gbd.repoSize, nil)
		if err != nil {
			return err
		}

		value = &DigestItem{
			expireTime:   gbd.clock.Now().Add(gbd.ttl),
			repositories: lru,
		}
	}

	if item.repo != nil {
		value.repositories.Add(*item.repo, struct{}{})
	}

	if item.desc != nil {
		value.desc = item.desc

		if dgst.Algorithm() != item.desc.Digest.Algorithm() && dgst != item.desc.Digest {
			// if the digests differ, set the other canonical mapping
			gbd.lru.Add(item.desc.Digest, value)
		}
	}

	gbd.lru.Add(dgst, value)

	return nil
}
