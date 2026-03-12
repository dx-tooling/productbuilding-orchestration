package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

type Handler struct {
	service *domain.Service
}

func NewHandler(service *domain.Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) ListPreviews(w http.ResponseWriter, r *http.Request) {
	previews, err := h.service.ListPreviews(r.Context())
	if err != nil {
		slog.Error("failed to list previews", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(previews); err != nil {
		slog.Error("failed to encode previews", "error", err)
	}
}

// GetLogs streams logs from a preview container.
// Query params: tail (int, default 100), follow (bool, default false)
func (h *Handler) GetLogs(w http.ResponseWriter, r *http.Request) {
	owner := r.PathValue("owner")
	repo := r.PathValue("repo")
	prStr := r.PathValue("pr")

	prNumber, err := strconv.Atoi(prStr)
	if err != nil {
		http.Error(w, "invalid PR number", http.StatusBadRequest)
		return
	}

	// Parse query params
	tail := 100
	if tailStr := r.URL.Query().Get("tail"); tailStr != "" {
		if t, err := strconv.Atoi(tailStr); err == nil && t > 0 {
			tail = t
		}
	}

	follow := false
	if followStr := r.URL.Query().Get("follow"); followStr != "" {
		follow = strings.ToLower(followStr) == "true" || followStr == "1"
	}

	// Set headers for streaming
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering if present

	if follow {
		w.Header().Set("Cache-Control", "no-cache")
	}

	// Stream logs
	ctx := r.Context()
	if err := h.service.GetPreviewLogs(ctx, owner, repo, prNumber, tail, follow, w); err != nil {
		slog.Error("failed to get preview logs", "error", err, "owner", owner, "repo", repo, "pr", prNumber)
		// Error already written to response, can't send HTTP error now
		return
	}
}
