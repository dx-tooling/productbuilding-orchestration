package web

import (
	"bytes"
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

// --- Mock Modal Opener ---

type mockModalOpener struct {
	called    bool
	triggerID string
	view      map[string]interface{}
	err       error
}

func (m *mockModalOpener) OpenView(ctx context.Context, botToken, triggerID string, view map[string]interface{}) error {
	m.called = true
	m.triggerID = triggerID
	m.view = view
	return m.err
}

// --- Tests for shortcut interactions ---

func TestHandleInteractions_CreatePlanShortcut_Success(t *testing.T) {
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
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		github,
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	// Build shortcut payload
	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "create_plan",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "1111111111.111111",
		"user": map[string]string{
			"id":   "U123",
			"name": "alice",
		},
		"response_url": "https://hooks.slack.com/commands/T123/123456",
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	// Should return 200 OK immediately
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Wait for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify GitHub comment was posted with correct format
	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	expectedBody := "/opencode Please write an implementation plan for this."
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}

	if github.number != 42 {
		t.Errorf("Expected issue #42, got %d", github.number)
	}

	// Verify public confirmation was posted
	if !poster.called {
		t.Error("Expected response_url to be called")
	}
}

func TestHandleInteractions_ImplementShortcut_Success(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 456}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			GithubPRID:    17,
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
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		github,
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "implement",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "1111111111.111111",
		"user": map[string]string{
			"id":   "U123",
			"name": "bob",
		},
		"response_url": "https://hooks.slack.com/commands/T123/123456",
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify GitHub comment with PR number
	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	expectedBody := "/opencode Please implement the plan."
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}

	if github.number != 17 { // Should use PR ID
		t.Errorf("Expected PR #17, got %d", github.number)
	}
}

func TestHandleInteractions_AddCommentShortcut_OpensModal(t *testing.T) {
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
	modalOpener := &mockModalOpener{}
	poster := &mockResponsePoster{}

	h := NewInteractionsHandler(
		threadFinder,
		&mockGitHubCommenter{},
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "add_comment",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "1111111111.111111",
		"trigger_id": "T1234567890.123456",
		"user": map[string]string{
			"id":   "U123",
			"name": "charlie",
		},
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	// Verify modal was opened
	if !modalOpener.called {
		t.Fatal("Expected modal to be opened")
	}

	if modalOpener.triggerID != "T1234567890.123456" {
		t.Errorf("Expected trigger_id T1234567890.123456, got %s", modalOpener.triggerID)
	}

	// Verify modal has correct structure
	view := modalOpener.view
	if view["type"] != "modal" {
		t.Errorf("Expected modal type, got %v", view["type"])
	}

	// Verify private_metadata contains thread info
	privateMeta, ok := view["private_metadata"].(string)
	if !ok || privateMeta == "" {
		t.Error("Expected private_metadata with thread info")
	}
}

func TestHandleInteractions_ViewSubmission_AddComment(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 789}
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
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		github,
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"test-workspace",
	)

	// Private metadata contains thread tracking info
	privateMeta := map[string]string{
		"thread_ts": "1111111111.111111",
		"channel":   "C123",
	}
	privateMetaJSON, _ := json.Marshal(privateMeta)

	payload := map[string]interface{}{
		"type": "view_submission",
		"view": map[string]interface{}{
			"callback_id":      "add_comment_modal",
			"private_metadata": string(privateMetaJSON),
			"state": map[string]interface{}{
				"values": map[string]interface{}{
					"comment_block": map[string]interface{}{
						"comment_input": map[string]interface{}{
							"value": "This is my comment text",
						},
					},
				},
			},
		},
		"user": map[string]string{
			"id":   "U123",
			"name": "dave",
		},
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(100 * time.Millisecond)

	// Verify GitHub comment was posted
	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	expectedBody := "This is my comment text"
	if !strings.Contains(github.body, expectedBody) {
		t.Errorf("Comment body should contain %q, got %q", expectedBody, github.body)
	}

	// Verify comment includes via-slack marker
	if !strings.Contains(github.body, "<!-- via-slack -->") {
		t.Error("Comment should contain via-slack marker")
	}

	// Note: For modal submissions, we don't have a response_url to post confirmation
	// The modal just closes (HTTP 200 response), which is the expected Slack behavior
}

func TestHandleInteractions_Shortcut_UntrackedThread(t *testing.T) {
	threadFinder := &mockThreadFinder{err: fmt.Errorf("thread not found")}
	poster := &mockResponsePoster{}
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		&mockGitHubCommenter{},
		nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "create_plan",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "9999999999.999999", // Unknown thread
		"user": map[string]string{
			"id":   "U123",
			"name": "eve",
		},
		"response_url": "https://hooks.slack.com/commands/T123/123456",
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	// Should post error to response_url
	if !poster.called {
		t.Error("Expected error response to be posted")
	}

	text, ok := poster.payload["text"].(string)
	if !ok || !strings.Contains(text, "not tracked") {
		t.Errorf("Expected error about untracked thread, got: %v", poster.payload)
	}
}

func TestHandleInteractions_Shortcut_NoTargetConfig(t *testing.T) {
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "unknown-owner",
			RepoName:      "unknown-repo",
			GithubIssueID: 42,
			SlackChannel:  "C123",
			SlackThreadTs: "1111111111.111111",
		},
	}
	registry := &mockTargetRegistry{
		found: false,
	}
	poster := &mockResponsePoster{}
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		&mockGitHubCommenter{},
		nil,
		&mockUserInfoResolver{},
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "create_plan",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "1111111111.111111",
		"user": map[string]string{
			"id":   "U123",
			"name": "frank",
		},
		"response_url": "https://hooks.slack.com/commands/T123/123456",
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	// Should post error to response_url
	if !poster.called {
		t.Error("Expected error response to be posted")
	}

	text, ok := poster.payload["text"].(string)
	if !ok || !strings.Contains(text, "configuration") {
		t.Errorf("Expected error about missing configuration, got: %v", poster.payload)
	}
}

func TestHandleInteractions_BadSignature(t *testing.T) {
	h := NewInteractionsHandler(
		nil, nil, nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		&mockResponsePoster{},
		&mockModalOpener{},
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "create_plan",
	}

	body, _ := json.Marshal(payload)
	timestamp := fmt.Sprintf("%d", time.Now().Unix())

	req := httptest.NewRequest("POST", "/slack/interactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", "v0=invalid-signature")

	rec := httptest.NewRecorder()
	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestHandleInteractions_UnknownCallbackID(t *testing.T) {
	poster := &mockResponsePoster{}
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		&mockThreadFinder{},
		&mockGitHubCommenter{},
		nil,
		&mockUserInfoResolver{},
		&mockTargetRegistry{},
		poster,
		modalOpener,
		testSigningSecret,
		"",
	)

	payload := map[string]interface{}{
		"type":        "shortcut",
		"callback_id": "unknown_callback",
		"channel": map[string]string{
			"id": "C123",
		},
		"message_ts": "1111111111.111111",
		"user": map[string]string{
			"id":   "U123",
			"name": "grace",
		},
		"response_url": "https://hooks.slack.com/commands/T123/123456",
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	// Should post error to response_url
	if !poster.called {
		t.Error("Expected error response to be posted")
	}

	text, ok := poster.payload["text"].(string)
	if !ok || !strings.Contains(text, "Unknown") {
		t.Errorf("Expected unknown callback error, got: %v", poster.payload)
	}
}

func TestHandleInteractions_AddCommentModal_UserNameResolution(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 101}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			SlackChannel:  "C123",
			SlackThreadTs: "1111111111.111111",
		},
	}
	userResolver := &mockUserInfoResolver{
		name: "Alice Smith",
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
	modalOpener := &mockModalOpener{}

	h := NewInteractionsHandler(
		threadFinder,
		github,
		nil,
		userResolver,
		registry,
		poster,
		modalOpener,
		testSigningSecret,
		"test-workspace",
	)

	privateMeta := map[string]string{
		"thread_ts":  "1111111111.111111",
		"channel":    "C123",
		"user_id":    "U123ALICE",
		"bot_token":  "xoxb-test",
		"github_pat": "ghp_test",
	}
	privateMetaJSON, _ := json.Marshal(privateMeta)

	payload := map[string]interface{}{
		"type": "view_submission",
		"view": map[string]interface{}{
			"callback_id":      "add_comment_modal",
			"private_metadata": string(privateMetaJSON),
			"state": map[string]interface{}{
				"values": map[string]interface{}{
					"comment_block": map[string]interface{}{
						"comment_input": map[string]interface{}{
							"value": "My resolved name comment",
						},
					},
				},
			},
		},
		"user": map[string]string{
			"id":   "U123ALICE",
			"name": "alice",
		},
	}

	req := makeSignedInteractionRequest(t, payload)
	rec := httptest.NewRecorder()

	h.HandleInteractions(rec, req)

	time.Sleep(100 * time.Millisecond)

	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	// Verify user display name was resolved
	if !strings.Contains(github.body, "**Alice Smith**") {
		t.Errorf("Expected resolved display name 'Alice Smith' in comment, got: %s", github.body)
	}
}

// --- Helper for interaction requests ---

func makeSignedInteractionRequest(t *testing.T, payload map[string]interface{}) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sigBase := "v0:" + timestamp + ":" + string(body)

	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/slack/interactions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	return req
}

// --- Helper for form-encoded interaction requests (alternative format) ---

func makeSignedFormInteractionRequest(t *testing.T, payload map[string]interface{}) *http.Request {
	t.Helper()

	payloadJSON, _ := json.Marshal(payload)
	formData := url.Values{}
	formData.Set("payload", string(payloadJSON))
	body := formData.Encode()

	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sigBase := "v0:" + timestamp + ":" + body

	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/slack/interactions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	return req
}
