package metrics

// Cache provides generic metrics for caches.
type Cache interface {
	Request(hit bool)
}

type cache struct {
	hitCounter  Counter
	missCounter Counter
}

func (c *cache) Request(hit bool) {
	if hit {
		c.hitCounter.Inc()
	} else {
		c.missCounter.Inc()
	}
}

type noopCache struct{}

func (c noopCache) Request(hit bool) {
}
