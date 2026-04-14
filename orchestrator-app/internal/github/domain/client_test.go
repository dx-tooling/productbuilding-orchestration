package domain

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestClient_GetCheckRunsForRef_403_ReturnsEmptySlice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	runs, err := client.GetCheckRunsForRef(context.Background(), "acme", "widgets", "abc123", "ghp_test123")
	if err != nil {
		t.Fatalf("GetCheckRunsForRef() should return nil error on 403, got: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0 on 403", len(runs))
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

// createTestTarGz constructs a valid tar.gz in memory with entries prefixed by rootDir/.
// This mimics GitHub's tarball format where everything is under "owner-repo-sha/".
func createTestTarGz(t *testing.T, rootDir string, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for path, content := range files {
		fullPath := rootDir + "/" + path
		if err := tw.WriteHeader(&tar.Header{
			Name:     fullPath,
			Mode:     0644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func TestClient_DownloadSource_Success(t *testing.T) {
	tarball := createTestTarGz(t, "acme-widgets-abc1234", map[string]string{
		"main.go":     "package main",
		"README.md":   "# Hello",
		"sub/file.go": "package sub",
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/tarball/abc1234f" {
			t.Errorf("Expected path /repos/acme/widgets/tarball/abc1234f, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ghp_test123" {
			t.Errorf("Expected Authorization Bearer ghp_test123, got %s", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write(tarball)
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}
	destDir := t.TempDir()

	result, err := client.DownloadSource(context.Background(), "acme", "widgets", "abc1234f", "ghp_test123", destDir)
	if err != nil {
		t.Fatalf("DownloadSource() error = %v", err)
	}
	if result != destDir {
		t.Errorf("DownloadSource() result = %q, want %q", result, destDir)
	}

	// Verify files were extracted with root directory stripped
	for _, path := range []string{"main.go", "README.md", "sub/file.go"} {
		fullPath := filepath.Join(destDir, path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			t.Errorf("Expected extracted file %s to exist", path)
		}
	}

	// Verify content
	content, err := os.ReadFile(filepath.Join(destDir, "main.go"))
	if err != nil {
		t.Fatalf("ReadFile error = %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("main.go content = %q, want %q", string(content), "package main")
	}
}

func TestClient_DownloadSource_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	_, err := client.DownloadSource(context.Background(), "acme", "widgets", "deadbeef", "ghp_test", t.TempDir())
	if err == nil {
		t.Error("DownloadSource() expected error for 404 response")
	}
}

func TestClient_CreateComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/issues/10/comments" {
			t.Errorf("Expected path /repos/acme/widgets/issues/10/comments, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer ghp_test123" {
			t.Errorf("Expected auth header, got %s", r.Header.Get("Authorization"))
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		if payload["body"] != "Preview deploying" {
			t.Errorf("Expected body 'Preview deploying', got %q", payload["body"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{"id": 999})
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	id, err := client.CreateComment(context.Background(), "acme", "widgets", 10, "Preview deploying", "ghp_test123")
	if err != nil {
		t.Fatalf("CreateComment() error = %v", err)
	}
	if id != 999 {
		t.Errorf("CreateComment() id = %d, want 999", id)
	}
}

func TestClient_CreateComment_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	_, err := client.CreateComment(context.Background(), "acme", "widgets", 10, "body", "ghp_test")
	if err == nil {
		t.Error("CreateComment() expected error for 403 response")
	}
}

func TestClient_UpdateComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("Expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/issues/comments/999" {
			t.Errorf("Expected path /repos/acme/widgets/issues/comments/999, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		if payload["body"] != "Updated text" {
			t.Errorf("Expected body 'Updated text', got %q", payload["body"])
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	err := client.UpdateComment(context.Background(), "acme", "widgets", 999, "Updated text", "ghp_test123")
	if err != nil {
		t.Fatalf("UpdateComment() error = %v", err)
	}
}

func TestClient_UpdateComment_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	err := client.UpdateComment(context.Background(), "acme", "widgets", 999, "body", "ghp_test")
	if err == nil {
		t.Error("UpdateComment() expected error for 404 response")
	}
}

func TestClient_DeleteComment_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("Expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/repos/acme/widgets/issues/comments/999" {
			t.Errorf("Expected path /repos/acme/widgets/issues/comments/999, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	err := client.DeleteComment(context.Background(), "acme", "widgets", 999, "ghp_test123")
	if err != nil {
		t.Fatalf("DeleteComment() error = %v", err)
	}
}

func TestClient_DeleteComment_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	err := client.DeleteComment(context.Background(), "acme", "widgets", 999, "ghp_test")
	if err == nil {
		t.Error("DeleteComment() expected error for 403 response")
	}
}

func TestClient_DeleteAllBotComments_FindsAndDeletesMarked(t *testing.T) {
	var deleteCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/comments"):
			// List comments: 2 with marker, 1 without
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{"id": 100, "body": "<!-- productbuilding-orchestrator -->\nPreview ready", "user": map[string]string{"login": "bot", "type": "Bot"}},
				{"id": 200, "body": "Nice work!", "user": map[string]string{"login": "alice", "type": "User"}},
				{"id": 300, "body": "<!-- productbuilding-orchestrator -->\nPreview failed", "user": map[string]string{"login": "bot", "type": "Bot"}},
			})

		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/issues/comments/"):
			deleteCount.Add(1)
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := &Client{httpClient: &http.Client{}, baseURL: server.URL}

	err := client.DeleteAllBotComments(context.Background(), "acme", "widgets", 10, "ghp_test123")
	if err != nil {
		t.Fatalf("DeleteAllBotComments() error = %v", err)
	}

	// Should delete exactly 2 comments (the ones with the marker)
	if deleteCount.Load() != 2 {
		t.Errorf("Expected 2 delete calls, got %d", deleteCount.Load())
	}
}
