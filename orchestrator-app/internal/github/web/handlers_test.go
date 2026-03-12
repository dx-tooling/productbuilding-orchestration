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
	"testing"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/domain"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/facade"
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
	notifyCalled bool
	events       []facade.NotificationEvent
	targets      []targets.TargetConfig
}

func (m *mockSlackNotifier) Notify(ctx context.Context, event facade.NotificationEvent, target targets.TargetConfig) error {
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
			RepoOwner:     "luminor-project",
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
			Owner: domain.User{Login: "luminor-project"},
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
			RepoOwner:     "luminor-project",
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
			Owner: domain.User{Login: "luminor-project"},
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
			RepoOwner:     "luminor-project",
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
			Owner: domain.User{Login: "luminor-project"},
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
			RepoOwner:     "luminor-project",
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
			Owner: domain.User{Login: "luminor-project"},
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
			RepoOwner:     "luminor-project",
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
