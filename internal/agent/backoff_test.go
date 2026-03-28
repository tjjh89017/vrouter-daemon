package agent

import (
	"testing"
	"time"
)

func TestBackoffExponentialGrowth(t *testing.T) {
	bo := newBackoff(1*time.Second, 30*time.Second)

	// Collect raw backoff values (before jitter) by checking bounds
	// With ±25% jitter, a 1s base should be in [0.75s, 1.25s]
	d := bo.Next()
	if d < 750*time.Millisecond || d > 1250*time.Millisecond {
		t.Fatalf("first backoff out of range: %v", d)
	}

	// Second: base 2s → [1.5s, 2.5s]
	d = bo.Next()
	if d < 1500*time.Millisecond || d > 2500*time.Millisecond {
		t.Fatalf("second backoff out of range: %v", d)
	}

	// Third: base 4s → [3s, 5s]
	d = bo.Next()
	if d < 3*time.Second || d > 5*time.Second {
		t.Fatalf("third backoff out of range: %v", d)
	}
}

func TestBackoffCapsAtMax(t *testing.T) {
	bo := newBackoff(1*time.Second, 5*time.Second)

	// Burn through several iterations
	for range 20 {
		bo.Next()
	}

	// Should be capped: base 5s → jitter [3.75s, 6.25s]
	d := bo.Next()
	if d > 6250*time.Millisecond {
		t.Fatalf("backoff exceeded max with jitter: %v", d)
	}
	if d < 3750*time.Millisecond {
		t.Fatalf("backoff too low: %v", d)
	}
}

func TestBackoffReset(t *testing.T) {
	bo := newBackoff(1*time.Second, 30*time.Second)

	// Advance several times
	bo.Next()
	bo.Next()
	bo.Next()

	bo.Reset()

	// Should be back to min: base 1s → [0.75s, 1.25s]
	d := bo.Next()
	if d < 750*time.Millisecond || d > 1250*time.Millisecond {
		t.Fatalf("after reset, backoff out of range: %v", d)
	}
}
