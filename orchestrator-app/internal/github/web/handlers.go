package web

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/domain"
)

type Handler struct {
	// webhookSecret will be per-target-repo in Phase 3; for now accept a single default.
	webhookSecret string
}

func NewHandler(webhookSecret string) *Handler {
	return &Handler{webhookSecret: webhookSecret}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Validate signature if a secret is configured.
	if h.webhookSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if err := domain.ValidateSignature(body, sig, h.webhookSecret); err != nil {
			slog.Warn("webhook signature validation failed", "error", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	eventType := r.Header.Get("X-GitHub-Event")
	if eventType != "pull_request" {
		slog.Debug("ignoring non-PR webhook event", "event", eventType)
		w.WriteHeader(http.StatusOK)
		return
	}

	event, err := domain.ParsePREvent(body)
	if err != nil {
		slog.Error("failed to parse PR event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	slog.Info("webhook received",
		"action", event.Action,
		"repo", event.RepoOwner+"/"+event.RepoName,
		"pr", event.PRNumber,
		"branch", event.Branch,
		"sha", event.HeadSHA,
	)

	// Phase 2: log only. Phase 3 will trigger preview lifecycle here.

	w.WriteHeader(http.StatusAccepted)
}
