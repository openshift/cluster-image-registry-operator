package counter

import (
	"fmt"
	"sync"
)

// M is a map with a counted values.
type M map[interface{}]int

// Difference represents a mismatch of an actual value and an expected value
// for a key in a counter.
type Difference struct {
	Key  interface{}
	Got  int
	Want int
}

func (d Difference) String() string {
	return fmt.Sprintf("%v: got %d, want %d", d.Key, d.Got, d.Want)
}

// Counter adds integers for different keys.
type Counter interface {
	// Add increments the value for key by delta.
	Add(key interface{}, delta int)

	// Values returns the counted values.
	Values() M

	// Diff returns the difference between the counted values and m. A return
	// value of nil indicates no difference.
	Diff(m M) []Difference
}

type counter struct {
	mu sync.Mutex
	m  M
}

// New returns a counter that is safe for concurrent use by multiple
// goroutines.
func New() Counter {
	return &counter{
		m: make(M),
	}
}

func (c *counter) Add(key interface{}, delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] += delta
}

func (c *counter) Values() M {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := make(map[interface{}]int)
	for k, v := range c.m {
		m[k] = v
	}
	return m
}

func (c *counter) Diff(m M) []Difference {
	c.mu.Lock()
	defer c.mu.Unlock()
	var diff []Difference
	for k, v := range m {
		if c.m[k] != v {
			diff = append(diff, Difference{Key: k, Got: c.m[k], Want: v})
		}
	}
	for k, v := range c.m {
		if want, ok := m[k]; !ok && v != 0 {
			diff = append(diff, Difference{Key: k, Got: v, Want: want})
		}
	}
	return diff
}
