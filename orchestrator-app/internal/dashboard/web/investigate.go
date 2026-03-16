package web

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
)

// TraceResult is the view model for displaying a trace in the investigation UI.
type TraceResult struct {
	ID            string
	RepoOwner     string
	RepoName      string
	GithubIssueID int
	SlackChannel  string
	SlackThreadTs string
	UserName      string
	UserText      string
	TraceData     string
	Error         string
	CreatedAt     time.Time
}

// TraceQuerier queries stored agent traces.
type TraceQuerier interface {
	FindByIssue(ctx context.Context, owner, repo string, issueID int) ([]TraceResult, error)
	FindBySlackThread(ctx context.Context, channel, threadTs string) ([]TraceResult, error)
}

// ParsedTrace is the deserialized trace data for display.
type ParsedTrace struct {
	TraceResult
	Trace *agent.Trace
}

var investigateFuncs = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
}

var investigateTmpl = template.Must(template.New("investigate").Funcs(investigateFuncs).Parse(`<!DOCTYPE html>
<html>
<head>
    <title>Investigation</title>
    <style>
        body { font-family: system-ui, sans-serif; max-width: 1100px; margin: 2rem auto; padding: 0 1rem; color: #333; }
        h1 { border-bottom: 2px solid #eee; padding-bottom: 0.5rem; }
        h2 { margin-top: 2rem; }
        .form-row { display: flex; gap: 0.5rem; margin-bottom: 1rem; }
        .form-row input[type=text] { flex: 1; padding: 0.5rem; font-size: 1rem; border: 1px solid #ccc; border-radius: 4px; }
        .form-row button { padding: 0.5rem 1.5rem; font-size: 1rem; background: #0366d6; color: white; border: none; border-radius: 4px; cursor: pointer; }
        .form-row button:hover { background: #0250a3; }
        .error { color: #721c24; background: #f8d7da; padding: 0.75rem; border-radius: 4px; margin-bottom: 1rem; }
        .empty { color: #999; font-style: italic; }
        .trace { border: 1px solid #ddd; border-radius: 6px; margin-bottom: 1.5rem; padding: 1rem; }
        .trace-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.75rem; }
        .trace-meta { color: #666; font-size: 0.85rem; }
        .trace-error { color: #721c24; font-weight: 600; }
        .trace-user { font-weight: 600; }
        .step { margin: 0.75rem 0; padding: 0.75rem; background: #f8f9fa; border-radius: 4px; }
        .step-header { font-weight: 600; color: #0366d6; margin-bottom: 0.5rem; }
        .iteration { margin: 0.5rem 0 0.5rem 1rem; padding: 0.5rem; border-left: 3px solid #ddd; }
        .tool-call { margin: 0.25rem 0 0.25rem 1rem; font-size: 0.9rem; }
        .tool-name { font-weight: 600; color: #6f42c1; }
        .tool-error { color: #721c24; }
        .tool-result { color: #155724; }
        pre { background: #f1f1f1; padding: 0.5rem; border-radius: 3px; overflow-x: auto; font-size: 0.8rem; max-height: 200px; overflow-y: auto; }
        .latency { color: #999; font-size: 0.8rem; }
        a { color: #0366d6; }
    </style>
</head>
<body>
    <h1><a href="/">Preview Orchestrator</a> / Investigation</h1>

    <form method="POST" action="/investigate">
        <div class="form-row">
            <input type="text" name="q" placeholder="#123, GitHub issue/PR URL, or Slack thread URL" value="{{.Query}}" autofocus>
            <button type="submit">Investigate</button>
        </div>
    </form>

    {{if .Error}}
    <div class="error">{{.Error}}</div>
    {{end}}

    {{if .Traces}}
    <h2>{{len .Traces}} trace(s) found</h2>
    {{range .Traces}}
    <div class="trace">
        <div class="trace-header">
            <div>
                <span class="trace-user">{{.UserName}}</span>: "{{.UserText}}"
                {{if .RepoOwner}}<span class="trace-meta">— {{.RepoOwner}}/{{.RepoName}}</span>{{end}}
                {{if gt .GithubIssueID 0}}<span class="trace-meta">#{{.GithubIssueID}}</span>{{end}}
            </div>
            <div class="trace-meta">{{.CreatedAt.Format "2006-01-02 15:04:05 UTC"}}</div>
        </div>

        {{if .Error}}<div class="trace-error">Error: {{.Error}}</div>{{end}}

        {{if .Trace}}
        {{if .Trace.Routing}}
        <div class="step">
            <div class="step-header">Routing</div>
            <div>{{.Trace.Routing.OutputText}}</div>
            {{if .Trace.Routing.LatencyMs}}<div class="latency">{{.Trace.Routing.LatencyMs}}ms</div>{{end}}
        </div>
        {{end}}

        {{range .Trace.Steps}}
        <div class="step">
            <div class="step-header">Specialist: {{.Specialist}}</div>
            {{range $i, $iter := .Iterations}}
            <div class="iteration">
                <div><strong>Iteration {{add $i 1}}</strong> ({{$iter.MessageCount}} messages, {{$iter.FinishReason}}) <span class="latency">{{$iter.LatencyMs}}ms</span></div>
                {{if $iter.LLMContent}}<pre>{{truncate $iter.LLMContent 500}}</pre>{{end}}
                {{range $iter.ToolCalls}}
                <div class="tool-call">
                    <span class="tool-name">{{.Name}}</span>
                    {{if .Arguments}}<pre>{{truncate .Arguments 300}}</pre>{{end}}
                    {{if .Error}}<div class="tool-error">Error: {{.Error}}</div>{{end}}
                    {{if .Result}}<pre>{{truncate .Result 500}}</pre>{{end}}
                    <span class="latency">{{.LatencyMs}}ms</span>
                </div>
                {{end}}
            </div>
            {{end}}
        </div>
        {{end}}
        {{end}}
    </div>
    {{end}}

    {{else if .Searched}}
    <p class="empty">No traces found.</p>
    {{end}}
</body>
</html>`))

// Investigate handles the investigation form POST.
func (h *Handler) Investigate(w http.ResponseWriter, r *http.Request) {
	query := r.FormValue("q")

	data := map[string]any{
		"Query":    query,
		"Traces":   []ParsedTrace(nil),
		"Error":    "",
		"Searched": false,
	}

	if query == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		investigateTmpl.Execute(w, data)
		return
	}

	parsed, err := ParseInvestigationInput(query)
	if err != nil {
		data["Error"] = err.Error()
		data["Searched"] = true
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		investigateTmpl.Execute(w, data)
		return
	}

	if h.traceQuerier == nil {
		data["Error"] = "Investigation not available (trace storage not configured)"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		investigateTmpl.Execute(w, data)
		return
	}

	var results []TraceResult
	switch parsed.Type {
	case QueryGitHub:
		results, err = h.traceQuerier.FindByIssue(r.Context(), parsed.Owner, parsed.Repo, parsed.Number)
	case QueryIssue:
		// For bare #N, search across all repos
		results, err = h.traceQuerier.FindByIssue(r.Context(), "", "", parsed.Number)
	case QuerySlack:
		threadTs := parsed.SlackThreadTs
		if threadTs == "" {
			threadTs = parsed.SlackTs
		}
		results, err = h.traceQuerier.FindBySlackThread(r.Context(), parsed.SlackChannel, threadTs)
	}

	if err != nil {
		slog.Error("investigation query failed", "error", err)
		data["Error"] = "Query failed: " + err.Error()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		investigateTmpl.Execute(w, data)
		return
	}

	// Parse trace JSON into structured data
	var parsed_traces []ParsedTrace
	for _, tr := range results {
		pt := ParsedTrace{TraceResult: tr}
		var trace agent.Trace
		if json.Unmarshal([]byte(tr.TraceData), &trace) == nil {
			pt.Trace = &trace
		}
		parsed_traces = append(parsed_traces, pt)
	}

	data["Traces"] = parsed_traces
	data["Searched"] = true
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	investigateTmpl.Execute(w, data)
}
