package domain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_CreateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/example-org/playground/issues" {
			t.Errorf("Expected path /repos/example-org/playground/issues, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ghp_test123" {
			t.Errorf("Expected Authorization Bearer ghp_test123, got %s", auth)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("Failed to parse request body: %v", err)
		}

		if payload["title"] != "Add dark mode support" {
			t.Errorf("Expected title 'Add dark mode support', got %q", payload["title"])
		}
		if payload["body"] != "Created via Slack" {
			t.Errorf("Expected body 'Created via Slack', got %q", payload["body"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number": 42,
		})
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	number, err := client.CreateIssue(context.Background(), "example-org", "playground", "Add dark mode support", "Created via Slack", "ghp_test123")
	if err != nil {
		t.Fatalf("CreateIssue() error = %v", err)
	}
	if number != 42 {
		t.Errorf("CreateIssue() number = %d, want 42", number)
	}
}

func TestClient_GetJobLogs(t *testing.T) {
	sampleLog := "2024-01-15T10:30:00.0000000Z Starting build\n2024-01-15T10:30:01.0000000Z ##[error]exit code 1\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/actions/jobs/200/logs" {
			t.Errorf("Expected path /repos/acme/widgets/actions/jobs/200/logs, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ghp_test123" {
			t.Errorf("Expected Authorization Bearer ghp_test123, got %s", auth)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sampleLog))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	log, err := client.GetJobLogs(context.Background(), "acme", "widgets", 200, "ghp_test123")
	if err != nil {
		t.Fatalf("GetJobLogs() error = %v", err)
	}
	if log != sampleLog {
		t.Errorf("GetJobLogs() = %q, want %q", log, sampleLog)
	}
}

func TestClient_GetPR_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/pulls/10" {
			t.Errorf("Expected path /repos/acme/widgets/pulls/10, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ghp_test123" {
			t.Errorf("Expected Authorization Bearer ghp_test123, got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"number":    10,
			"title":     "Add dark mode",
			"body":      "Fixes #5",
			"state":     "open",
			"merged":    false,
			"html_url":  "https://github.com/acme/widgets/pull/10",
			"additions": 50,
			"deletions": 10,
			"head": map[string]interface{}{
				"sha": "abc123def456",
				"ref": "feature-dark-mode",
			},
			"base": map[string]interface{}{
				"ref": "main",
			},
			"user": map[string]interface{}{
				"login": "alice",
			},
		})
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	pr, err := client.GetPR(context.Background(), "acme", "widgets", 10, "ghp_test123")
	if err != nil {
		t.Fatalf("GetPR() error = %v", err)
	}
	if pr.Number != 10 {
		t.Errorf("Number = %d, want 10", pr.Number)
	}
	if pr.Title != "Add dark mode" {
		t.Errorf("Title = %q, want %q", pr.Title, "Add dark mode")
	}
	if pr.Body != "Fixes #5" {
		t.Errorf("Body = %q, want %q", pr.Body, "Fixes #5")
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want %q", pr.State, "open")
	}
	if pr.Merged {
		t.Error("Merged = true, want false")
	}
	if pr.HeadSHA != "abc123def456" {
		t.Errorf("HeadSHA = %q, want %q", pr.HeadSHA, "abc123def456")
	}
	if pr.HeadRef != "feature-dark-mode" {
		t.Errorf("HeadRef = %q, want %q", pr.HeadRef, "feature-dark-mode")
	}
	if pr.BaseRef != "main" {
		t.Errorf("BaseRef = %q, want %q", pr.BaseRef, "main")
	}
	if pr.URL != "https://github.com/acme/widgets/pull/10" {
		t.Errorf("URL = %q, want %q", pr.URL, "https://github.com/acme/widgets/pull/10")
	}
	if pr.User != "alice" {
		t.Errorf("User = %q, want %q", pr.User, "alice")
	}
	if pr.Additions != 50 {
		t.Errorf("Additions = %d, want 50", pr.Additions)
	}
	if pr.Deletions != 10 {
		t.Errorf("Deletions = %d, want 10", pr.Deletions)
	}
}

func TestClient_GetPR_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	_, err := client.GetPR(context.Background(), "acme", "widgets", 999, "ghp_test123")
	if err == nil {
		t.Error("GetPR() expected error for 404 response")
	}
}

func TestClient_GetCheckRunsForRef_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/commits/abc123/check-runs" {
			t.Errorf("Expected path /repos/acme/widgets/commits/abc123/check-runs, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ghp_test123" {
			t.Errorf("Expected Authorization Bearer ghp_test123, got %s", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"check_runs": []map[string]interface{}{
				{
					"id":         int64(1001),
					"name":       "build",
					"status":     "completed",
					"conclusion": "success",
					"html_url":   "https://github.com/acme/widgets/runs/1001",
				},
				{
					"id":         int64(1002),
					"name":       "lint",
					"status":     "completed",
					"conclusion": "failure",
					"html_url":   "https://github.com/acme/widgets/runs/1002",
				},
			},
		})
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	runs, err := client.GetCheckRunsForRef(context.Background(), "acme", "widgets", "abc123", "ghp_test123")
	if err != nil {
		t.Fatalf("GetCheckRunsForRef() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(runs))
	}

	if runs[0].ID != 1001 {
		t.Errorf("runs[0].ID = %d, want 1001", runs[0].ID)
	}
	if runs[0].Name != "build" {
		t.Errorf("runs[0].Name = %q, want %q", runs[0].Name, "build")
	}
	if runs[0].Status != "completed" {
		t.Errorf("runs[0].Status = %q, want %q", runs[0].Status, "completed")
	}
	if runs[0].Conclusion != "success" {
		t.Errorf("runs[0].Conclusion = %q, want %q", runs[0].Conclusion, "success")
	}
	if runs[0].HTMLURL != "https://github.com/acme/widgets/runs/1001" {
		t.Errorf("runs[0].HTMLURL = %q, want %q", runs[0].HTMLURL, "https://github.com/acme/widgets/runs/1001")
	}

	if runs[1].ID != 1002 {
		t.Errorf("runs[1].ID = %d, want 1002", runs[1].ID)
	}
	if runs[1].Name != "lint" {
		t.Errorf("runs[1].Name = %q, want %q", runs[1].Name, "lint")
	}
	if runs[1].Conclusion != "failure" {
		t.Errorf("runs[1].Conclusion = %q, want %q", runs[1].Conclusion, "failure")
	}
}

func TestClient_GetCheckRunsForRef_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"check_runs": []map[string]interface{}{},
		})
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	runs, err := client.GetCheckRunsForRef(context.Background(), "acme", "widgets", "abc123", "ghp_test123")
	if err != nil {
		t.Fatalf("GetCheckRunsForRef() error = %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0", len(runs))
	}
}

func TestClient_CreateIssue_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation Failed"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	_, err := client.CreateIssue(context.Background(), "owner", "repo", "Title", "Body", "pat")
	if err == nil {
		t.Error("CreateIssue() expected error for 422 response")
	}
}
