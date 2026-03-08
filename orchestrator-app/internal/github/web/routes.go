package web

import "net/http"

func RegisterRoutes(mux *http.ServeMux, webhookSecret string) {
	h := NewHandler(webhookSecret)
	mux.HandleFunc("POST /webhook", h.HandleWebhook)
}
