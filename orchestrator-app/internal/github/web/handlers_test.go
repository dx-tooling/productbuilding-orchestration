package web

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// generateSignature creates a valid GitHub webhook signature for testing
func generateSignature(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Mock implementations
type mockPreviewService struct {
	deployCalled bool
	deployReq    previewdomain.DeployRequest
	deleteCalled bool
	deleteReq    previewdomain.DeployRequest
}

func (m *mockPreviewService) DeployPreview(ctx context.Context, req previewdomain.DeployRequest, pat string) {
	m.deployCalled = true
	m.deployReq = req
}

func (m *mockPreviewService) DeletePreview(ctx context.Context, req previewdomain.DeployRequest, pat string) {
	m.deleteCalled = true
	m.deleteReq = req
}

type mockSlackNotifier struct {
	mu           sync.Mutex
	notifyCalled bool
	events       []facade.NotificationEvent
	targets      []targets.TargetConfig
}

func (m *mockSlackNotifier) Notify(ctx context.Context, event facade.NotificationEvent, target targets.TargetConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifyCalled = true
	m.events = append(m.events, event)
	m.targets = append(m.targets, target)
	return nil
}

type mockTargetRegistry struct {
	config targets.TargetConfig
	found  bool
}

func (m *mockTargetRegistry) Get(repoOwner, repoName string) (targets.TargetConfig, bool) {
	return m.config, m.found
}

func TestHandleWebhook_IssueOpened(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding-test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	// Create issue opened payload
	payload := domain.IssueEvent{
		Action: "opened",
		Issue: domain.Issue{
			Number: 42,
			Title:  "Add dark mode support",
			Body:   "We need dark mode for better UX",
			User:   domain.User{Login: "alice"},
		},
		Repository: domain.Repository{
			Owner: domain.User{Login: "example-org"},
			Name:  "test-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Wait for async notification to complete
	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Error("Expected Slack notification to be sent")
	}

	if len(slackNotifier.events) != 1 {
		t.Fatalf("Expected 1 Slack event, got %d", len(slackNotifier.events))
	}

	event := slackNotifier.events[0]
	if event.Type != facade.EventIssueOpened {
		t.Errorf("Expected event type %s, got %s", facade.EventIssueOpened, event.Type)
	}
	if event.IssueNumber != 42 {
		t.Errorf("Expected issue number 42, got %d", event.IssueNumber)
	}
	if event.Title != "Add dark mode support" {
		t.Errorf("Expected title 'Add dark mode support', got %s", event.Title)
	}
	if event.Author != "alice" {
		t.Errorf("Expected author 'alice', got %s", event.Author)
	}
}

func TestHandleWebhook_IssueCommentCreated(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding-test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := domain.IssueCommentEvent{
		Action: "created",
		Comment: domain.Comment{
			ID:   123456,
			Body: "This looks great! 🎉",
			User: domain.User{Login: "bob"},
		},
		Issue: domain.Issue{
			Number: 42,
			Title:  "Add dark mode support",
		},
		Repository: domain.Repository{
			Owner: domain.User{Login: "example-org"},
			Name:  "test-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Wait for async notification to complete
	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Error("Expected Slack notification to be sent")
	}

	if len(slackNotifier.events) != 1 {
		t.Fatalf("Expected 1 Slack event, got %d", len(slackNotifier.events))
	}

	event := slackNotifier.events[0]
	if event.Type != facade.EventCommentAdded {
		t.Errorf("Expected event type %s, got %s", facade.EventCommentAdded, event.Type)
	}
	if event.CommentID != 123456 {
		t.Errorf("Expected comment ID 123456, got %d", event.CommentID)
	}
	if event.Body != "This looks great! 🎉" {
		t.Errorf("Expected body 'This looks great! 🎉', got %s", event.Body)
	}
}

func TestHandleWebhook_PROpened_PassesLinkedIssueNumber(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "playground",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	// PR #17 with "Fixes #16" in body
	payload := map[string]interface{}{
		"action": "opened",
		"pull_request": map[string]interface{}{
			"number": 17,
			"title":  "Added tech/arch section to homepage",
			"body":   "Fixes #16\n\nAdded technical architecture section",
			"user":   map[string]string{"login": "opencode-agent[bot]"},
			"head": map[string]string{
				"sha": "c07b81d7",
				"ref": "feature/homepage-tech",
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "example-org"},
			"name":  "playground",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Wait for async notification
	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Fatal("Expected Slack notification to be sent for PR opened")
	}

	if len(slackNotifier.events) < 1 {
		t.Fatal("Expected at least 1 Slack event")
	}

	event := slackNotifier.events[0]
	if event.Type != facade.EventPROpened {
		t.Errorf("Expected event type %s, got %s", facade.EventPROpened, event.Type)
	}
	if event.IssueNumber != 17 {
		t.Errorf("Expected IssueNumber 17, got %d", event.IssueNumber)
	}
	if event.LinkedIssueNumber != 16 {
		t.Errorf("Expected LinkedIssueNumber 16 (from 'Fixes #16' in body), got %d", event.LinkedIssueNumber)
	}
	if event.Title != "Added tech/arch section to homepage" {
		t.Errorf("Expected Title to be set, got %q", event.Title)
	}
	if event.Author != "opencode-agent[bot]" {
		t.Errorf("Expected Author to be set, got %q", event.Author)
	}
}

func TestHandleWebhook_PRMerged_NotifiesSlack(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "playground",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := map[string]interface{}{
		"action": "closed",
		"pull_request": map[string]interface{}{
			"number": 17,
			"title":  "Added tech/arch section",
			"body":   "Fixes #16",
			"merged": true,
			"user":   map[string]string{"login": "alice"},
			"head": map[string]string{
				"sha": "c07b81d7",
				"ref": "feature/homepage-tech",
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "example-org"},
			"name":  "playground",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Fatal("Expected Slack notification for PR merged")
	}

	if len(slackNotifier.events) < 1 {
		t.Fatal("Expected at least 1 Slack event")
	}

	event := slackNotifier.events[len(slackNotifier.events)-1]
	if event.Type != facade.EventPRMerged {
		t.Errorf("Expected event type %s, got %s", facade.EventPRMerged, event.Type)
	}
	if event.IssueNumber != 17 {
		t.Errorf("Expected IssueNumber 17, got %d", event.IssueNumber)
	}
}

func TestHandleWebhook_PRClosedNotMerged_NotifiesSlack(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "playground",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := map[string]interface{}{
		"action": "closed",
		"pull_request": map[string]interface{}{
			"number": 17,
			"title":  "Abandoned PR",
			"body":   "",
			"merged": false,
			"user":   map[string]string{"login": "alice"},
			"head": map[string]string{
				"sha": "c07b81d7",
				"ref": "feature/abandoned",
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "example-org"},
			"name":  "playground",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Fatal("Expected Slack notification for PR closed")
	}

	event := slackNotifier.events[len(slackNotifier.events)-1]
	if event.Type != facade.EventPRClosed {
		t.Errorf("Expected event type %s, got %s", facade.EventPRClosed, event.Type)
	}
}

func TestHandleWebhook_IssueCommentFromSlack_SkipsNotification(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding-test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	// Comment body contains the via-slack marker
	payload := domain.IssueCommentEvent{
		Action: "created",
		Comment: domain.Comment{
			ID:   789,
			Body: "**@Alice** via Slack:\n\nplease fix the alignment\n\n<!-- via-slack -->",
			User: domain.User{Login: "orchestrator-bot"},
		},
		Issue: domain.Issue{
			Number: 42,
			Title:  "Add dark mode support",
		},
		Repository: domain.Repository{
			Owner: domain.User{Login: "example-org"},
			Name:  "test-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Wait for async
	time.Sleep(100 * time.Millisecond)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	slackNotifier.mu.Lock()
	called := slackNotifier.notifyCalled
	slackNotifier.mu.Unlock()

	if called {
		t.Error("Should NOT send Slack notification for comment originated from Slack (via-slack marker)")
	}
}

func TestHandleWebhook_UnknownRepo(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		found: false,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := domain.IssueEvent{
		Action: "opened",
		Issue:  domain.Issue{Number: 42},
		Repository: domain.Repository{
			Owner: domain.User{Login: "unknown"},
			Name:  "unknown-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Should return 404
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	// Should not notify Slack
	if slackNotifier.notifyCalled {
		t.Error("Should not send Slack notification for unknown repo")
	}
}

func TestHandleWebhook_NoSlackConfig(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			// No SlackChannel or SlackBotToken
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := domain.IssueEvent{
		Action: "opened",
		Issue:  domain.Issue{Number: 42, Title: "Test issue"},
		Repository: domain.Repository{
			Owner: domain.User{Login: "example-org"},
			Name:  "test-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Should still return 202 (webhook processed)
	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", rec.Code)
	}

	// Should NOT notify Slack (no config)
	if slackNotifier.notifyCalled {
		t.Error("Should not send Slack notification when no Slack config")
	}
}

func TestHandleWebhook_InvalidSignature(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#productbuilding-test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload := domain.IssueEvent{
		Action: "opened",
		Issue:  domain.Issue{Number: 42},
		Repository: domain.Repository{
			Owner: domain.User{Login: "example-org"},
			Name:  "test-repo",
		},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=invalid-signature")

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Should return 401
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401 for invalid signature, got %d", rec.Code)
	}
}

func TestHandleWebhook_UnsupportedEvent(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GitHubPAT:     "github_pat_test",
			WebhookSecret: "secret123",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	body := []byte(`{}`)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push") // Unsupported event

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	// Should return 200 but do nothing
	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if slackNotifier.notifyCalled {
		t.Error("Should not notify for unsupported events")
	}
}

func TestHandleWebhook_CheckRun_Failure_NotifiesSlack(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "acme",
			RepoName:      "widgets",
			GitHubPAT:     "ghp_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload, _ := json.Marshal(map[string]interface{}{
		"action": "completed",
		"check_run": map[string]interface{}{
			"id":         1001,
			"name":       "build",
			"status":     "completed",
			"conclusion": "failure",
			"html_url":   "https://github.com/acme/widgets/runs/1001",
			"head_sha":   "abc123",
			"pull_requests": []map[string]interface{}{
				{"number": 10},
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "acme"},
			"name":  "widgets",
		},
	})

	sig := generateSignature(payload, "secret123")
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.HandleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", rec.Code)
	}

	// Wait for async goroutine
	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Fatal("Expected Slack notification for check_run failure")
	}
	if len(slackNotifier.events) == 0 {
		t.Fatal("Expected at least one event")
	}
	ev := slackNotifier.events[0]
	if ev.Type != facade.EventCIFailed {
		t.Errorf("Expected EventCIFailed, got %s", ev.Type)
	}
	if ev.CheckRunName != "build" {
		t.Errorf("Expected CheckRunName=build, got %s", ev.CheckRunName)
	}
	if ev.IssueNumber != 10 {
		t.Errorf("Expected IssueNumber=10, got %d", ev.IssueNumber)
	}
}

func TestHandleWebhook_CheckRun_Success_NotifiesSlack(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "acme",
			RepoName:      "widgets",
			GitHubPAT:     "ghp_test",
			WebhookSecret: "secret123",
			SlackChannel:  "#test",
			SlackBotToken: "xoxb-test",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload, _ := json.Marshal(map[string]interface{}{
		"action": "completed",
		"check_run": map[string]interface{}{
			"id":         1002,
			"name":       "lint",
			"status":     "completed",
			"conclusion": "success",
			"html_url":   "https://github.com/acme/widgets/runs/1002",
			"head_sha":   "abc123",
			"pull_requests": []map[string]interface{}{
				{"number": 10},
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "acme"},
			"name":  "widgets",
		},
	})

	sig := generateSignature(payload, "secret123")
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if !slackNotifier.notifyCalled {
		t.Fatal("Expected Slack notification for check_run success")
	}
	ev := slackNotifier.events[0]
	if ev.Type != facade.EventCIPassed {
		t.Errorf("Expected EventCIPassed, got %s", ev.Type)
	}
}

func TestHandleWebhook_CheckRun_InProgress_Ignored(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "acme",
			RepoName:      "widgets",
			WebhookSecret: "secret123",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload, _ := json.Marshal(map[string]interface{}{
		"action": "created",
		"check_run": map[string]interface{}{
			"id":     1003,
			"name":   "build",
			"status": "in_progress",
			"pull_requests": []map[string]interface{}{
				{"number": 10},
			},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "acme"},
			"name":  "widgets",
		},
	})

	sig := generateSignature(payload, "secret123")
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for in_progress, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if slackNotifier.notifyCalled {
		t.Error("Should not notify for in_progress check runs")
	}
}

// mockAgentInvoker records calls to InvokeForEvent
type mockAgentInvoker struct {
	mu    sync.Mutex
	calls []facade.NotificationEvent
}

func (m *mockAgentInvoker) InvokeForEvent(ctx context.Context, event facade.NotificationEvent, target targets.TargetConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, event)
}

func TestHandleWebhook_IssueComment_BotSenderSkipped(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	agentInvoker := &mockAgentInvoker{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:      "example-org",
			RepoName:       "test-repo",
			GitHubPAT:      "github_pat_test",
			WebhookSecret:  "secret123",
			SlackChannel:   "#productbuilding-test",
			SlackBotToken:  "xoxb-test",
			BotGitHubLogin: "PrdctBldr",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier, agentInvoker)

	// Comment from the bot account — no via-agent/via-slack markers
	payload := map[string]interface{}{
		"action": "created",
		"comment": map[string]interface{}{
			"id":   789,
			"body": "Preview deploying for commit abc123...",
			"user": map[string]string{"login": "PrdctBldr"},
		},
		"issue": map[string]interface{}{
			"number": 42,
			"title":  "Add dark mode support",
		},
		"sender":     map[string]string{"login": "PrdctBldr"},
		"repository": map[string]interface{}{"owner": map[string]string{"login": "example-org"}, "name": "test-repo"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	slackNotifier.mu.Lock()
	notified := slackNotifier.notifyCalled
	slackNotifier.mu.Unlock()

	if notified {
		t.Error("Should NOT send Slack notification for bot-sender comment")
	}

	agentInvoker.mu.Lock()
	invoked := len(agentInvoker.calls)
	agentInvoker.mu.Unlock()

	if invoked > 0 {
		t.Error("Should NOT invoke agent for bot-sender comment")
	}
}

func TestHandleWebhook_IssueComment_OpenCodeBot_InvokesAgent(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	agentInvoker := &mockAgentInvoker{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:      "example-org",
			RepoName:       "test-repo",
			GitHubPAT:      "github_pat_test",
			WebhookSecret:  "secret123",
			SlackChannel:   "#productbuilding-test",
			SlackBotToken:  "xoxb-test",
			BotGitHubLogin: "PrdctBldr",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier, agentInvoker)

	// Comment from opencode-agent[bot] — not the bot account
	payload := map[string]interface{}{
		"action": "created",
		"comment": map[string]interface{}{
			"id":   790,
			"body": "Created PR #96",
			"user": map[string]string{"login": "opencode-agent[bot]"},
		},
		"issue": map[string]interface{}{
			"number": 42,
			"title":  "Add dark mode support",
		},
		"sender":     map[string]string{"login": "opencode-agent[bot]"},
		"repository": map[string]interface{}{"owner": map[string]string{"login": "example-org"}, "name": "test-repo"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issue_comment")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	slackNotifier.mu.Lock()
	notified := slackNotifier.notifyCalled
	slackNotifier.mu.Unlock()

	if !notified {
		t.Error("Should send Slack notification for opencode-agent[bot] comment")
	}

	agentInvoker.mu.Lock()
	invoked := len(agentInvoker.calls)
	agentInvoker.mu.Unlock()

	if invoked != 1 {
		t.Errorf("Expected 1 agent invocation for opencode-agent[bot] comment, got %d", invoked)
	}
}

func TestHandleWebhook_PRMerged_InvokesAgent(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	agentInvoker := &mockAgentInvoker{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:      "example-org",
			RepoName:       "playground",
			GitHubPAT:      "github_pat_test",
			WebhookSecret:  "secret123",
			SlackChannel:   "#productbuilding",
			SlackBotToken:  "xoxb-test",
			BotGitHubLogin: "PrdctBldr",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier, agentInvoker)

	payload := map[string]interface{}{
		"action": "closed",
		"pull_request": map[string]interface{}{
			"number": 17,
			"title":  "Added tech/arch section",
			"body":   "Fixes #16",
			"merged": true,
			"user":   map[string]string{"login": "alice"},
			"head":   map[string]string{"sha": "c07b81d7", "ref": "feature/homepage-tech"},
		},
		"sender":     map[string]string{"login": "alice"},
		"repository": map[string]interface{}{"owner": map[string]string{"login": "example-org"}, "name": "playground"},
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature(body, "secret123"))

	rec := httptest.NewRecorder()
	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	agentInvoker.mu.Lock()
	defer agentInvoker.mu.Unlock()

	if len(agentInvoker.calls) != 1 {
		t.Errorf("Expected 1 agent invocation for PR merged, got %d", len(agentInvoker.calls))
	}
	if len(agentInvoker.calls) > 0 && agentInvoker.calls[0].Type != facade.EventPRMerged {
		t.Errorf("Expected EventPRMerged, got %s", agentInvoker.calls[0].Type)
	}
}

func TestHandleWebhook_CIFailed_InvokesAgent(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	agentInvoker := &mockAgentInvoker{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:      "acme",
			RepoName:       "widgets",
			GitHubPAT:      "ghp_test",
			WebhookSecret:  "secret123",
			SlackChannel:   "#test",
			SlackBotToken:  "xoxb-test",
			BotGitHubLogin: "PrdctBldr",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier, agentInvoker)

	payload, _ := json.Marshal(map[string]interface{}{
		"action": "completed",
		"check_run": map[string]interface{}{
			"id": 1001, "name": "build", "status": "completed", "conclusion": "failure",
			"html_url": "https://github.com/acme/widgets/runs/1001", "head_sha": "abc123",
			"pull_requests": []map[string]interface{}{{"number": 10}},
		},
		"sender":     map[string]string{"login": "github-actions[bot]"},
		"repository": map[string]interface{}{"owner": map[string]string{"login": "acme"}, "name": "widgets"},
	})

	sig := generateSignature(payload, "secret123")
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.HandleWebhook(rec, req)

	time.Sleep(100 * time.Millisecond)

	agentInvoker.mu.Lock()
	defer agentInvoker.mu.Unlock()

	if len(agentInvoker.calls) != 1 {
		t.Errorf("Expected 1 agent invocation for CI failed, got %d", len(agentInvoker.calls))
	}
	if len(agentInvoker.calls) > 0 && agentInvoker.calls[0].Type != facade.EventCIFailed {
		t.Errorf("Expected EventCIFailed, got %s", agentInvoker.calls[0].Type)
	}
}

func TestHandleWebhook_CheckRun_NoPR_Ignored(t *testing.T) {
	previewSvc := &mockPreviewService{}
	slackNotifier := &mockSlackNotifier{}
	registry := &mockTargetRegistry{
		config: targets.TargetConfig{
			RepoOwner:     "acme",
			RepoName:      "widgets",
			WebhookSecret: "secret123",
		},
		found: true,
	}

	handler := NewHandler(registry, previewSvc, slackNotifier)

	payload, _ := json.Marshal(map[string]interface{}{
		"action": "completed",
		"check_run": map[string]interface{}{
			"id":            1004,
			"name":          "build",
			"status":        "completed",
			"conclusion":    "failure",
			"pull_requests": []map[string]interface{}{},
		},
		"repository": map[string]interface{}{
			"owner": map[string]string{"login": "acme"},
			"name":  "widgets",
		},
	})

	sig := generateSignature(payload, "secret123")
	req := httptest.NewRequest("POST", "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()

	handler.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 for no-PR check run, got %d", rec.Code)
	}

	time.Sleep(50 * time.Millisecond)

	slackNotifier.mu.Lock()
	defer slackNotifier.mu.Unlock()

	if slackNotifier.notifyCalled {
		t.Error("Should not notify for check runs without linked PRs")
	}
}
