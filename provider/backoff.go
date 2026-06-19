package provider

import (
	"context"
	"time"
)

const (
	// backoffBase is the initial wait before a provider re-establishes a dropped
	// watch or subscription.
	backoffBase = 500 * time.Millisecond
	// backoffMax caps the exponential backoff between reconnect attempts.
	backoffMax = 30 * time.Second
)

// Backoff returns how long to wait before the given reconnect attempt. The
// attempt is zero-based: attempt 0 waits backoffBase, and each subsequent
// attempt doubles the wait up to backoffMax. Providers use it to avoid hammering
// a backend that is temporarily unavailable.
func Backoff(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := backoffBase
	for range attempt {
		d *= 2
		if d >= backoffMax {
			return backoffMax
		}
	}
	return d
}

// SleepBackoff blocks for the backoff duration of the given attempt, returning
// true once the wait elapses. It returns false immediately if ctx is cancelled
// during the wait, so callers can use it as their loop's exit signal.
func SleepBackoff(ctx context.Context, attempt int) bool {
	t := time.NewTimer(Backoff(attempt))
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
