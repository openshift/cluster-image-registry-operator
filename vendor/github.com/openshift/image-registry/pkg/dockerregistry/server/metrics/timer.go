package metrics

import (
	"time"
)

// Timer is a helper type to time functions.
type Timer interface {
	// Stop records the duration passed since the Timer was created with NewTimer.
	Stop()
}

// NewTimer wraps the HistogramVec and used to track amount of time passed since the Timer was created.
func NewTimer(observer Observer) Timer {
	return &timer{
		observer:  observer,
		startTime: time.Now(),
	}
}

type timer struct {
	observer  Observer
	startTime time.Time
}

func (t *timer) Stop() {
	t.observer.Observe(time.Since(t.startTime).Seconds())
}
