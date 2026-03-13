package domain

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// RetryConfig controls retry behavior for HTTP requests.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns sensible defaults for LLM API calls.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// isRetryable returns true for HTTP status codes that warrant a retry.
func isRetryable(statusCode int) bool {
	switch statusCode {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// backoff calculates the delay for a given retry attempt using exponential backoff with jitter.
func backoff(attempt int, cfg RetryConfig) time.Duration {
	exp := math.Pow(2, float64(attempt))
	base := time.Duration(float64(cfg.BaseDelay) * exp)
	if base > cfg.MaxDelay {
		base = cfg.MaxDelay
	}
	// Add jitter: random duration between 0 and base
	jitter := time.Duration(rand.Int63n(int64(base) + 1))
	return jitter
}

// sleepWithContext sleeps for the given duration or until the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
