package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type mockTraceQuerier struct {
	traces []TraceResult
	err    error
}

func (m *mockTraceQuerier) FindByIssue(_ context.Context, _, _ string, _ int) ([]TraceResult, error) {
	return m.traces, m.err
}

func (m *mockTraceQuerier) FindBySlackThread(_ context.Context, _, _ string) ([]TraceResult, error) {
	return m.traces, m.err
}

func TestInvestigate_GitHubIssueURL(t *testing.T) {
	traces := []TraceResult{
		{
			ID:        "trace-1",
			UserName:  "Alice",
			UserText:  "check CI status",
			TraceData: `{"Routing":{"OutputText":"steps: [researcher]"},"Steps":[]}`,
			CreatedAt: time.Now(),
		},
	}
	querier := &mockTraceQuerier{traces: traces}
	h := NewHandler(nil)
	h.traceQuerier = querier

	form := url.Values{"q": {"https://github.com/luminor-project/playground/issues/57"}}
	req := httptest.NewRequest("POST", "/investigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Investigate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Alice") {
		t.Errorf("expected trace user name in output, got:\n%s", body)
	}
	if !strings.Contains(body, "check CI status") {
		t.Errorf("expected user text in output, got:\n%s", body)
	}
}

func TestInvestigate_IssueNumber(t *testing.T) {
	querier := &mockTraceQuerier{traces: []TraceResult{}}
	h := NewHandler(nil)
	h.traceQuerier = querier

	form := url.Values{"q": {"#57"}}
	req := httptest.NewRequest("POST", "/investigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Investigate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No traces found") {
		t.Errorf("expected 'No traces found' message for empty results")
	}
}

func TestInvestigate_InvalidInput(t *testing.T) {
	h := NewHandler(nil)

	form := url.Values{"q": {"random gibberish"}}
	req := httptest.NewRequest("POST", "/investigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Investigate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unrecognized") {
		t.Errorf("expected error message about unrecognized input")
	}
}

func TestInvestigate_SlackURL(t *testing.T) {
	traces := []TraceResult{
		{
			ID:        "trace-2",
			UserName:  "Bob",
			UserText:  "what happened?",
			TraceData: `{"Steps":[]}`,
			CreatedAt: time.Now(),
		},
	}
	querier := &mockTraceQuerier{traces: traces}
	h := NewHandler(nil)
	h.traceQuerier = querier

	form := url.Values{"q": {"https://luminor-tech.slack.com/archives/C0AL8824SBH/p1773605494857279?thread_ts=1773578348.731309&cid=C0AL8824SBH"}}
	req := httptest.NewRequest("POST", "/investigate", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.Investigate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Bob") {
		t.Errorf("expected trace user name in output")
	}
}
