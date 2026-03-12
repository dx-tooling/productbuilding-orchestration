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
	"strconv"
	"testing"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

const testSigningSecret = "test-signing-secret-123"

// --- Mock implementations ---

type mockThreadFinder struct {
	thread *domain.SlackThread
	err    error
}

func (m *mockThreadFinder) FindThreadBySlackTs(ctx context.Context, threadTs string) (*domain.SlackThread, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.thread, nil
}

type mockGitHubCommenter struct {
	called    bool
	owner     string
	repo      string
	number    int
	body      string
	pat       string
	commentID int64
	err       error
}

func (m *mockGitHubCommenter) CreateComment(ctx context.Context, owner, repo string, number int, body, pat string) (int64, error) {
	m.called = true
	m.owner = owner
	m.repo = repo
	m.number = number
	m.body = body
	m.pat = pat
	return m.commentID, m.err
}

type mockUserInfoResolver struct {
	name string
	err  error
}

func (m *mockUserInfoResolver) GetUserInfo(ctx context.Context, botToken, userID string) (string, error) {
	return m.name, m.err
}

type mockTargetRegistry struct {
	config targets.TargetConfig
	found  bool
}

func (m *mockTargetRegistry) Get(repoOwner, repoName string) (targets.TargetConfig, bool) {
	return m.config, m.found
}

// --- Helpers ---

func signRequest(body []byte, timestamp string) string {
	sigBase := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func makeSignedRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", signRequest(body, timestamp))
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	return req
}

// --- Tests ---

func TestHandleEvent_URLVerification(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, testSigningSecret, "")

	payload := map[string]string{
		"type":      "url_verification",
		"challenge": "abc123challenge",
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp["challenge"] != "abc123challenge" {
		t.Errorf("Expected challenge abc123challenge, got %s", resp["challenge"])
	}
}

func TestHandleEvent_BadSignature(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", "v0=invalid-signature")
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401, got %d", rec.Code)
	}
}

func TestHandleEvent_StaleTimestamp(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	// Timestamp 10 minutes ago
	staleTs := strconv.FormatInt(time.Now().Unix()-600, 10)

	sigBase := "v0:" + staleTs + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(testSigningSecret))
	mac.Write([]byte(sigBase))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	req.Header.Set("X-Slack-Signature", sig)
	req.Header.Set("X-Slack-Request-Timestamp", staleTs)

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for stale timestamp, got %d", rec.Code)
	}
}

func TestHandleEvent_MissingSignatureHeaders(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil, testSigningSecret, "")

	body := []byte(`{"type":"url_verification","challenge":"test"}`)
	req := httptest.NewRequest("POST", "/slack/events", bytes.NewReader(body))
	// No signature headers set

	rec := httptest.NewRecorder()
	h.HandleEvent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for missing headers, got %d", rec.Code)
	}
}

func TestHandleEvent_AppMentionWithoutThreadTs(t *testing.T) {
	github := &mockGitHubCommenter{}
	h := NewHandler(&mockThreadFinder{}, github, &mockUserInfoResolver{}, &mockTargetRegistry{}, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> hello",
			"channel": "C123",
			"ts":      "1234567890.123456",
			// No thread_ts — top-level mention
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	// Give goroutine time to not fire
	time.Sleep(50 * time.Millisecond)

	if github.called {
		t.Error("Should not post GitHub comment for mention without thread_ts")
	}
}

func TestHandleEvent_AppMentionInUntrackedThread(t *testing.T) {
	github := &mockGitHubCommenter{}
	threadFinder := &mockThreadFinder{err: fmt.Errorf("thread not found")}
	h := NewHandler(threadFinder, github, &mockUserInfoResolver{}, &mockTargetRegistry{}, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> hello",
			"thread_ts": "9999999999.999999",
			"channel":   "C123",
			"ts":        "1234567890.123456",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	if github.called {
		t.Error("Should not post GitHub comment for untracked thread")
	}
}

func TestHandleEvent_AppMentionInTrackedThread(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 99}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			SlackChannel:  "#productbuilding",
			SlackThreadTs: "1111111111.111111",
		},
	}
	userResolver := &mockUserInfoResolver{name: "Alice Smith"}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GitHubPAT:     "ghp_test123",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	h := NewHandler(threadFinder, github, userResolver, registry, testSigningSecret, "test-workspace")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123ALICE",
			"text":      "<@UBOT> please fix the alignment",
			"thread_ts": "1111111111.111111",
			"channel":   "C123",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rec.Code)
	}

	// Wait for async goroutine
	time.Sleep(100 * time.Millisecond)

	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	if github.owner != "luminor-project" || github.repo != "playground" {
		t.Errorf("Wrong repo: %s/%s", github.owner, github.repo)
	}
	if github.number != 42 {
		t.Errorf("Expected issue number 42, got %d", github.number)
	}
	if github.pat != "ghp_test123" {
		t.Errorf("Expected PAT ghp_test123, got %s", github.pat)
	}

	// Verify comment format (includes deep link to Slack message)
	expectedBody := "**Alice Smith** [via Slack](https://test-workspace.slack.com/archives/C123/p2222222222222222?thread_ts=1111111111.111111&cid=C123):\n\nplease fix the alignment\n\n<!-- via-slack -->"
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}
}

func TestHandleEvent_AppMentionInTrackedThread_UsesPRID(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 99}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			GithubPRID:    17, // PR phase — should use PR ID
			SlackChannel:  "#productbuilding",
			SlackThreadTs: "1111111111.111111",
		},
	}
	userResolver := &mockUserInfoResolver{name: "Bob"}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GitHubPAT:     "ghp_test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	h := NewHandler(threadFinder, github, userResolver, registry, testSigningSecret, "test-workspace")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123BOB",
			"text":      "<@UBOT> looks good",
			"thread_ts": "1111111111.111111",
			"channel":   "C123",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	time.Sleep(100 * time.Millisecond)

	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}
	if github.number != 17 {
		t.Errorf("Expected PR number 17, got %d", github.number)
	}
}

func TestHandleEvent_BotMentionStripped(t *testing.T) {
	github := &mockGitHubCommenter{commentID: 99}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			RepoOwner:     "luminor-project",
			RepoName:      "playground",
			GithubIssueID: 42,
			SlackThreadTs: "1111111111.111111",
		},
	}
	userResolver := &mockUserInfoResolver{name: "Charlie"}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			GitHubPAT:     "ghp_test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	h := NewHandler(threadFinder, github, userResolver, registry, testSigningSecret, "test-workspace")

	// Text with bot mention at different positions
	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOTID> hey <@UBOTID> do the thing",
			"thread_ts": "1111111111.111111",
			"channel":   "C123",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOTID"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)

	time.Sleep(100 * time.Millisecond)

	if !github.called {
		t.Fatal("Expected GitHub comment to be posted")
	}

	// The mention should be stripped; text should just be "hey  do the thing" trimmed
	expectedBody := "**Charlie** [via Slack](https://test-workspace.slack.com/archives/C123/p2222222222222222?thread_ts=1111111111.111111&cid=C123):\n\nhey  do the thing\n\n<!-- via-slack -->"
	if github.body != expectedBody {
		t.Errorf("Unexpected comment body:\ngot:  %q\nwant: %q", github.body, expectedBody)
	}
}
