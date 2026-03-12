package web

import "net/http"

// RegisterRoutes registers Slack Events API routes on the given mux
func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	mux.HandleFunc("POST /slack/events", handler.HandleEvent)
}
