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
