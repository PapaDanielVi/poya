package provider_test

import (
	"context"
	"testing"
	"time"

	"github.com/PapaDanielVi/poya/provider"
)

func TestBackoff(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"negative clamps to base", -1, 500 * time.Millisecond},
		{"attempt 0 is base", 0, 500 * time.Millisecond},
		{"attempt 1 doubles", 1, time.Second},
		{"attempt 2 doubles again", 2, 2 * time.Second},
		{"large attempt caps at max", 100, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := provider.Backoff(tc.attempt); got != tc.want {
				t.Errorf("Backoff(%d) = %v, want %v", tc.attempt, got, tc.want)
			}
		})
	}
}

func TestSleepBackoffReturnsFalseOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if provider.SleepBackoff(ctx, 0) {
		t.Error("SleepBackoff should return false when the context is already cancelled")
	}
}

func TestSleepBackoffReturnsTrueWhenElapsed(t *testing.T) {
	t.Parallel()
	// attempt 0 waits backoffBase (500ms); a generous timeout context still lets
	// the timer fire first and return true.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !provider.SleepBackoff(ctx, 0) {
		t.Error("SleepBackoff should return true once the backoff elapses")
	}
}
