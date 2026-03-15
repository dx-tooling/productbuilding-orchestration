package web

import (
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, previewService *domain.Service) {
	h := NewHandler(previewService)
	mux.HandleFunc("GET /", h.ShowDashboard)
}
