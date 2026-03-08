package web

import (
	"encoding/json"
	"log/slog"
	"net/http"

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
