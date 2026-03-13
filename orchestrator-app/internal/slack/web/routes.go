package web

import "net/http"

// RegisterRoutes registers Slack Events API routes on the given mux
func RegisterRoutes(mux *http.ServeMux, handler *Handler) {
	mux.HandleFunc("POST /slack/events", handler.HandleEvent)
}

// RegisterSlashCommandRoutes registers Slack slash command routes on the given mux
func RegisterSlashCommandRoutes(mux *http.ServeMux, slashHandler *SlashCommandHandler) {
	mux.HandleFunc("POST /slack/commands", slashHandler.HandleSlashCommand)
}

// RegisterInteractionsRoutes registers Slack interactions routes on the given mux
func RegisterInteractionsRoutes(mux *http.ServeMux, interactionsHandler *InteractionsHandler) {
	mux.HandleFunc("POST /slack/interactions", interactionsHandler.HandleInteractions)
}
