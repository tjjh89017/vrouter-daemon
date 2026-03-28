package agent

import (
	"math/rand/v2"
	"time"
)

// backoff implements exponential backoff with jitter, similar to
// k8s client-go's wait.Backoff.
//
// Each call to Next() returns the current wait duration and advances
// the state. The duration doubles each step (up to max), with ±25%
// jitter to avoid thundering herd.
//
// Call Reset() on successful connection to restart from min.
type backoff struct {
	min     time.Duration
	max     time.Duration
	current time.Duration
}

func newBackoff(min, max time.Duration) *backoff {
	return &backoff{
		min:     min,
		max:     max,
		current: min,
	}
}

// Next returns the current backoff duration (with jitter) and advances
// the internal state for the next call.
func (b *backoff) Next() time.Duration {
	d := jitter(b.current)

	// Advance for next call
	b.current *= 2
	if b.current > b.max {
		b.current = b.max
	}

	return d
}

// Reset restarts backoff from min (call on successful connection).
func (b *backoff) Reset() {
	b.current = b.min
}

// jitter adds ±25% randomness to avoid thundering herd.
func jitter(d time.Duration) time.Duration {
	// [0.75, 1.25) * d
	factor := 0.75 + rand.Float64()*0.5
	return time.Duration(float64(d) * factor)
}
