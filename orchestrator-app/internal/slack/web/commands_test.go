package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// --- Mock Response Poster (for async Slack notifications) ---

type mockResponsePoster struct {
	called      bool
	responseURL string
	payload     map[string]interface{}
	err         error
}

func (m *mockResponsePoster) PostResponse(ctx context.Context, responseURL string, payload map[string]interface{}) error {
	m.called = true
	m.responseURL = responseURL
	m.payload = payload
	return m.err
}

// --- Tests for /create-issue ---

func TestHandleSlashCommand_CreateIssue_Success(t *testing.T) {
	issueCreator := &mockGitHubIssueCreator{number: 42}
	userResolver := &mockUserInfoResolver{
		name:        "Alice Smith",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: targets.TargetConfig{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GitHubPAT:     "ghp_test123",
			SlackBotToken: "xoxb-test",
		},
		channelFound: true,
		botToken:     "xoxb-test",
	}
	poster := &mockResponsePoster{}

	h := NewSlashCommandHandler(
		nil, // threadFinder not needed for create-issue
		nil, // githubClient not needed for create-issue
		issueCreator,
		userResolver,
		registry,
		poster,
		testSigningSecret,
		"test-workspace",
	)

	// Simulate Slack slash command payload
	formData := url.Values{}
	formData.Set("command", "/create-issue")
	formData.Set("text", "Add dark mode support")
	formData.Set("channel_id", "C0PRODUCT")
	formData.Set("channel_name", "productbuilding-playground")
	formData.Set("user_id", "U123ALICE")
	formData.Set("user_name", "alice")
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	// Should return 200 OK immediately with ephemeral message
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Verify immediate response is ephemeral JSON
	contentType := rec.Header().Get("Content-Type")
	if !strings.Contains(contentType, "application/json") {
		t.Errorf("Expected JSON content type, got %s", contentType)
	}

	var immediateResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &immediateResp); err != nil {
		t.Fatalf("Failed to parse immediate response: %v", err)
	}

	if immediateResp["response_type"] != "ephemeral" {
		t.Errorf("Expected ephemeral response_type, got %v", immediateResp["response_type"])
	}

	// Wait for async goroutine
	time.Sleep(100 * time.Millisecond)

	// Verify GitHub issue was created
	if !issueCreator.called {
		t.Fatal("Expected GitHub issue to be created")
	}
	if issueCreator.title != "Add dark mode support" {
		t.Errorf("Expected title 'Add dark mode support', got %q", issueCreator.title)
	}
	if issueCreator.owner != "luminor-project" || issueCreator.repo != "playground" {
		t.Errorf("Wrong repo: %s/%s", issueCreator.owner, issueCreator.repo)
	}

	// Verify response_url was called with public confirmation
	if !poster.called {
		t.Error("Expected response_url to be called for public confirmation")
	}
	if poster.responseURL != "https://hooks.slack.com/commands/T123/123456" {
		t.Errorf("Wrong response_url: %s", poster.responseURL)
	}
}

func TestHandleSlashCommand_CreateIssue_MissingText(t *testing.T) {
	registry := &mockTargetRegistry{
		channelConfig: targets.TargetConfig{
			SlackBotToken: "xoxb-test",
		},
		channelFound: true,
		botToken:     "xoxb-test",
	}

	h := NewSlashCommandHandler(
		nil, nil, nil,
		&mockUserInfoResolver{channelName: "productbuilding-playground"},
		registry,
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-issue")
	formData.Set("text", "") // Empty text
	formData.Set("channel_id", "C0PRODUCT")
	formData.Set("channel_name", "productbuilding-playground")
	formData.Set("user_id", "U123")
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["response_type"] != "ephemeral" {
		t.Error("Expected ephemeral error response")
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "usage") && !strings.Contains(text, "required") {
		t.Errorf("Expected usage error message, got: %s", text)
	}
}

func TestHandleSlashCommand_CreateIssue_UnknownChannel(t *testing.T) {
	registry := &mockTargetRegistry{
		channelFound: false,
		botToken:     "xoxb-test",
	}
	userResolver := &mockUserInfoResolver{channelName: "random-channel"}

	h := NewSlashCommandHandler(
		nil, nil, nil,
		userResolver,
		registry,
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-issue")
	formData.Set("text", "Some issue")
	formData.Set("channel_id", "C0UNKNOWN")
	formData.Set("channel_name", "random-channel")
	formData.Set("user_id", "U123")
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "not configured") && !strings.Contains(text, "not tracked") {
		t.Errorf("Expected error about untracked channel, got: %s", text)
	}
}

// --- Tests for /create-plan ---

func TestHandleSlashCommand_CreatePlan_Success(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 123}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			SlackChannel:  "C123",
			SlackThreadTs: "1111111111.111111",
		},
	}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GitHubPAT:     "ghp_test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}
	poster := &mockResponsePoster{}

	h := NewSlashCommandHandler(
		threadFinder,
		github,
		nil, // issueCreator not needed
		&mockUserInfoResolver{},
		registry,
		poster,
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-plan")
	formData.Set("text", "make sure to add e2e tests")
	formData.Set("channel_id", "C123")
	formData.Set("channel_name", "productbuilding-playground")
	formData.Set("user_id", "U123")
	formData.Set("user_name", "alice")
	formData.Set("thread_ts", "1111111111.111111") // Must be in thread
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Wait for async
	time.Sleep(100 * time.Millisecond)

	// Verify GitHub comment was posted with correct format
	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	expectedBody := "/opencode Please write an implementation plan for this. make sure to add e2e tests"
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}

	if github.number != 42 {
		t.Errorf("Expected issue #42, got %d", github.number)
	}

	// Verify public confirmation
	if !poster.called {
		t.Error("Expected response_url to be called")
	}
}

func TestHandleSlashCommand_CreatePlan_NotInThread(t *testing.T) {
	h := NewSlashCommandHandler(
		nil, nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-plan")
	formData.Set("text", "add tests")
	formData.Set("channel_id", "C123")
	formData.Set("channel_name", "productbuilding-playground")
	formData.Set("user_id", "U123")
	formData.Set("user_name", "alice")
	// No thread_ts — not in thread
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "thread") {
		t.Errorf("Expected error about thread requirement, got: %s", text)
	}
}

func TestHandleSlashCommand_CreatePlan_UntrackedThread(t *testing.T) {
	threadFinder := &mockThreadFinder{err: fmt.Errorf("thread not found")}

	h := NewSlashCommandHandler(
		threadFinder,
		nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-plan")
	formData.Set("text", "add tests")
	formData.Set("channel_id", "C123")
	formData.Set("user_id", "U123")
	formData.Set("thread_ts", "9999999999.999999") // Unknown thread
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "tracked") {
		t.Errorf("Expected error about untracked thread, got: %s", text)
	}
}

// --- Tests for /implement ---

func TestHandleSlashCommand_Implement_Success(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 456}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			GithubPRID:    17, // PR exists — should use PR ID
			SlackChannel:  "C123",
			SlackThreadTs: "1111111111.111111",
		},
	}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GitHubPAT:     "ghp_test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}
	poster := &mockResponsePoster{}

	h := NewSlashCommandHandler(
		threadFinder,
		github,
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/implement")
	formData.Set("text", "focus on performance")
	formData.Set("channel_id", "C123")
	formData.Set("user_id", "U123")
	formData.Set("user_name", "bob")
	formData.Set("thread_ts", "1111111111.111111")
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify GitHub comment with PR number (not issue number)
	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	expectedBody := "/opencode Please implement the plan. focus on performance"
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}

	if github.number != 17 { // Should use PR ID, not issue ID
		t.Errorf("Expected PR #17, got %d", github.number)
	}
}

func TestHandleSlashCommand_Implement_NotInThread(t *testing.T) {
	h := NewSlashCommandHandler(
		nil, nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/implement")
	formData.Set("text", "")
	formData.Set("channel_id", "C123")
	formData.Set("user_id", "U123")
	// No thread_ts
	formData.Set("response_url", "https://hooks.slack.com/commands/T123/123456")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "thread") {
		t.Errorf("Expected error about thread requirement, got: %s", text)
	}
}

// --- Security Tests ---

func TestHandleSlashCommand_BadSignature(t *testing.T) {
	h := NewSlashCommandHandler(
		nil, nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/create-issue")
	formData.Set("text", "test")

	req := httptest.NewRequest("POST", "/slack/commands", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Signature", "v0=invalid-signature")
	req.Header.Set("X-Slack-Request-Timestamp", fmt.Sprintf("%d", time.Now().Unix()))

	rec := httptest.NewRecorder()
	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestHandleSlashCommand_UnknownCommand(t *testing.T) {
	h := NewSlashCommandHandler(
		nil, nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		testSigningSecret,
		"",
	)

	formData := url.Values{}
	formData.Set("command", "/unknown-command")
	formData.Set("text", "test")
	formData.Set("channel_id", "C123")
	formData.Set("user_id", "U123")

	req := makeSignedFormRequest(t, "/slack/commands", formData.Encode())
	rec := httptest.NewRecorder()

	h.HandleSlashCommand(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	text, _ := resp["text"].(string)
	if !strings.Contains(text, "unknown") || !strings.Contains(text, "command") {
		t.Errorf("Expected unknown command error, got: %s", text)
	}
}

// --- Helper for form-encoded requests ---

func makeSignedFormRequest(t *testing.T, path, body string) *http.Request {
	t.Helper()
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	// Calculate signature for form-encoded body
	sigBase := "v0:" + timestamp + ":" + body
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))
	req.Header.Set("X-Slack-Signature", sig)

	return req
}
