package infra

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HealthChecker polls an HTTP endpoint until it responds with a success status.
type HealthChecker struct {
	httpClient *http.Client
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// WaitForHealthy polls the given URL until it returns 2xx/3xx or the timeout expires.
func (h *HealthChecker) WaitForHealthy(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	slog.Info("waiting for health check", "url", url, "timeout", timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("health check timed out after %s", timeout)
		case <-ticker.C:
			resp, err := h.httpClient.Get(url)
			if err != nil {
				slog.Debug("health check not ready", "url", url, "error", err)
				continue
			}
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				slog.Info("health check passed", "url", url, "status", resp.StatusCode)
				return nil
			}
			slog.Debug("health check non-ok", "url", url, "status", resp.StatusCode)
		}
	}
}
