package web

import (
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, previewService *domain.Service, traceQuerier TraceQuerier) {
	h := NewHandler(previewService)
	h.traceQuerier = traceQuerier
	mux.HandleFunc("GET /", h.ShowDashboard)
	mux.HandleFunc("POST /investigate", h.Investigate)
	mux.HandleFunc("GET /investigate", h.Investigate)
}
