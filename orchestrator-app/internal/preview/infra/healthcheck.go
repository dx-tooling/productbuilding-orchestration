package infra

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// HealthChecker polls endpoints until they respond successfully.
type HealthChecker struct {
	httpClient *http.Client
	// tlsClient uses the default TLS config (validates certificates).
	tlsClient *http.Client
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		tlsClient: &http.Client{
			Timeout: 5 * time.Second,
			// Default transport validates TLS certificates.
			// A TLS handshake failure (self-signed/not-yet-provisioned)
			// surfaces as an error from client.Get.
		},
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

// WaitForTLS polls the given HTTPS URL until the TLS handshake succeeds with a
// valid certificate. This catches the window where Traefik serves its default
// self-signed cert while the Let's Encrypt DNS-01 challenge is still in progress.
func (h *HealthChecker) WaitForTLS(ctx context.Context, url string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	slog.Info("waiting for valid TLS certificate", "url", url, "timeout", timeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("TLS readiness timed out after %s", timeout)
		case <-ticker.C:
			if err := checkTLS(url); err != nil {
				slog.Debug("TLS not ready", "url", url, "error", err)
				continue
			}
			slog.Info("TLS certificate valid", "url", url)
			return nil
		}
	}
}

// checkTLS does a TLS handshake against the URL and verifies the certificate
// chain is valid (not self-signed, not expired).
func checkTLS(url string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// Use default verification — rejects self-signed certs.
			},
		},
		// Don't follow redirects; we only care about the TLS handshake.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
