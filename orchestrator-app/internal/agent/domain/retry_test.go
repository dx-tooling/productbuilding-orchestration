package domain

import (
	"context"
	"testing"
	"time"
)

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		status    int
		retryable bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		if got := isRetryable(tt.status); got != tt.retryable {
			t.Errorf("isRetryable(%d) = %v, want %v", tt.status, got, tt.retryable)
		}
	}
}

func TestBackoff(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}

	// Attempt 0: ~100ms base, attempt 1: ~200ms, attempt 2: ~400ms
	for attempt := 0; attempt < 3; attempt++ {
		d := backoff(attempt, cfg)
		// With jitter, the delay should be between 0 and 2x the exponential base
		maxExpected := cfg.BaseDelay * (1 << attempt) * 2
		if d < 0 || d > maxExpected {
			t.Errorf("backoff(%d) = %v, expected between 0 and %v", attempt, d, maxExpected)
		}
	}
}

func TestBackoff_CappedAtMaxDelay(t *testing.T) {
	cfg := RetryConfig{
		MaxRetries: 10,
		BaseDelay:  1 * time.Second,
		MaxDelay:   5 * time.Second,
	}

	// At attempt 10, exponential would be 1024s, but should be capped at 5s
	d := backoff(10, cfg)
	if d > cfg.MaxDelay {
		t.Errorf("backoff(10) = %v, expected at most %v", d, cfg.MaxDelay)
	}
}

func TestSleepWithContext_Completes(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	err := sleepWithContext(ctx, 10*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if elapsed < 10*time.Millisecond {
		t.Errorf("slept for %v, expected at least 10ms", elapsed)
	}
}

func TestSleepWithContext_CancelledEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := sleepWithContext(ctx, 5*time.Second)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected context error, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("slept for %v, expected early cancellation", elapsed)
	}
}

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries 3, got %d", cfg.MaxRetries)
	}
	if cfg.BaseDelay != 1*time.Second {
		t.Errorf("expected BaseDelay 1s, got %v", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 30*time.Second {
		t.Errorf("expected MaxDelay 30s, got %v", cfg.MaxDelay)
	}
}
