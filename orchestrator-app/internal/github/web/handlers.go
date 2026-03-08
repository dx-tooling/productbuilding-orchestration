package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/domain"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

type Handler struct {
	registry       *targets.Registry
	previewService *previewdomain.Service
}

func NewHandler(registry *targets.Registry, previewService *previewdomain.Service) *Handler {
	return &Handler{
		registry:       registry,
		previewService: previewService,
	}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
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

	// Look up target config for this repo
	target, ok := h.registry.Get(event.RepoOwner, event.RepoName)
	if !ok {
		slog.Warn("webhook from unknown repo", "repo", event.RepoOwner+"/"+event.RepoName)
		http.Error(w, "unknown repository", http.StatusNotFound)
		return
	}

	// Validate webhook signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if err := domain.ValidateSignature(body, sig, target.WebhookSecret); err != nil {
		slog.Warn("webhook signature validation failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("webhook received",
		"action", event.Action,
		"repo", event.RepoOwner+"/"+event.RepoName,
		"pr", event.PRNumber,
		"branch", event.Branch,
		"sha", event.HeadSHA,
	)

	req := previewdomain.DeployRequest{
		RepoOwner: event.RepoOwner,
		RepoName:  event.RepoName,
		PRNumber:  event.PRNumber,
		Branch:    event.Branch,
		HeadSHA:   event.HeadSHA,
	}

	switch event.Action {
	case "opened", "synchronize", "reopened":
		// Deploy/update preview asynchronously
		go h.previewService.DeployPreview(context.Background(), req, target.GitHubPAT)
	case "closed":
		// Tear down preview asynchronously
		go h.previewService.DeletePreview(context.Background(), req, target.GitHubPAT)
	default:
		slog.Debug("ignoring PR action", "action", event.Action)
	}

	w.WriteHeader(http.StatusAccepted)
}
