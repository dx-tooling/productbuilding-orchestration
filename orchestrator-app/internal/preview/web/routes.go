package web

import (
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, service *domain.Service) {
	h := NewHandler(service)
	mux.HandleFunc("GET /previews", h.ListPreviews)
	mux.HandleFunc("GET /previews/{owner}/{repo}/{pr}/logs", h.GetLogs)
}
