package web

import (
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, registry *targets.Registry, previewService *previewdomain.Service, notifier Notifier) {
	h := NewHandler(registry, previewService, notifier)
	mux.HandleFunc("POST /webhook", h.HandleWebhook)
}
