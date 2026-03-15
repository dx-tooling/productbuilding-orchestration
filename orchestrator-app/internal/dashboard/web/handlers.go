package web

import (
	"html/template"
	"log/slog"
	"net/http"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

var dashboardTmpl = template.Must(template.New("dashboard").Parse(`<!DOCTYPE html>
<html>
<head>
    <title>Preview Orchestrator</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 960px; margin: 2rem auto; padding: 0 1rem; color: #333; }
        h1 { border-bottom: 2px solid #eee; padding-bottom: 0.5rem; }
        table { width: 100%; border-collapse: collapse; }
        th, td { text-align: left; padding: 0.5rem; border-bottom: 1px solid #eee; }
        th { font-weight: 600; color: #666; }
        .status { padding: 0.2rem 0.5rem; border-radius: 3px; font-size: 0.85rem; font-weight: 500; }
        .status-ready { background: #d4edda; color: #155724; }
        .status-building, .status-deploying, .status-pending { background: #fff3cd; color: #856404; }
        .status-failed { background: #f8d7da; color: #721c24; }
        a { color: #0366d6; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .empty { color: #999; font-style: italic; padding: 2rem 0; }
    </style>
</head>
<body>
    <h1>Preview Orchestrator</h1>
    {{if .Previews}}
    <table>
        <thead>
            <tr><th>Repo</th><th>PR</th><th>Branch</th><th>Status</th><th>SHA</th><th>Preview</th><th>Updated</th></tr>
        </thead>
        <tbody>
            {{range .Previews}}
            <tr>
                <td>{{.RepoOwner}}/{{.RepoName}}</td>
                <td>#{{.PRNumber}}</td>
                <td>{{.BranchName}}</td>
                <td><span class="status status-{{.Status}}">{{.Status}}</span></td>
                <td><code>{{slice .HeadSHA 0 7}}</code></td>
                <td>{{if eq (printf "%s" .Status) "ready"}}<a href="{{.PreviewURL}}" target="_blank">Open</a>{{else}}&mdash;{{end}}</td>
                <td>{{.UpdatedAt.Format "2006-01-02 15:04"}}</td>
            </tr>
            {{end}}
        </tbody>
    </table>
    {{else}}
    <p class="empty">No active previews.</p>
    {{end}}
</body>
</html>`))

type Handler struct {
	previewService *domain.Service
}

func NewHandler(previewService *domain.Service) *Handler {
	return &Handler{previewService: previewService}
}

func (h *Handler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	previews, err := h.previewService.ListPreviews(r.Context())
	if err != nil {
		slog.Error("failed to list previews for dashboard", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTmpl.Execute(w, map[string]any{"Previews": previews}); err != nil {
		slog.Error("failed to render dashboard", "error", err)
	}
}
