package web

import (
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func RegisterRoutes(mux *http.ServeMux, registry *targets.Registry, previewService *previewdomain.Service, notifier Notifier, agentInvoker ...AgentInvoker) {
	var ai []AgentInvoker
	if len(agentInvoker) > 0 {
		ai = agentInvoker
	}
	h := NewHandler(registry, previewService, notifier, ai...)
	mux.HandleFunc("POST /webhook", h.HandleWebhook)
}
