package web

import (
	"net/http"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, registry *targets.Registry, previewService *previewdomain.Service, notifier Notifier) {
	h := NewHandler(registry, previewService, notifier)
	mux.HandleFunc("POST /webhook", h.HandleWebhook)
}
