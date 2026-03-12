package domain

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// TestNotifier_UsesCorrectBotTokenFromTarget verifies that the notifier
// uses the bot token from TargetConfig, not from client initialization.
// This is a regression test for the "not_authed" bug where the client
// was initialized with an empty token and never updated.
func TestNotifier_UsesCorrectBotTokenFromTarget(t *testing.T) {
	// Create a mock client that records which token was used
	var usedToken string
	mockClient := &tokenRecordingClient{
		onPostMessage: func(botToken string) {
			usedToken = botToken
		},
	}

	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(mockClient, repo, debouncer)

	// Target config with a specific bot token
	target := targets.TargetConfig{
		RepoOwner:     "test-owner",
		RepoName:      "test-repo",
		GitHubPAT:     "ghp_test",
		WebhookSecret: "secret",
		SlackChannel:  "#test-channel",
		SlackBotToken: "xoxb-specific-token-12345",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Test Issue",
		Body:        "Test body",
		Author:      "testuser",
	}

	ctx := context.Background()
	err := notifier.Notify(ctx, event, target)
	if err != nil {
		t.Errorf("Notify() error = %v", err)
	}

	// Execute the debounced function immediately
	debouncer.executeAll()

	// Verify the correct token was used
	if usedToken == "" {
		t.Fatal("No token was passed to Slack client - bug regression!")
	}
	if usedToken != target.SlackBotToken {
		t.Errorf("Wrong token used: got %q, want %q", usedToken, target.SlackBotToken)
	}
}

// TestNotifier_SkipsWhenNoSlackConfig verifies that notifications are silently
// skipped when Slack config is missing, without errors.
func TestNotifier_SkipsWhenNoSlackConfig(t *testing.T) {
	mockClient := &tokenRecordingClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(mockClient, repo, debouncer)

	// Target without Slack config
	target := targets.TargetConfig{
		RepoOwner:     "test-owner",
		RepoName:      "test-repo",
		GitHubPAT:     "ghp_test",
		WebhookSecret: "secret",
		// SlackChannel and SlackBotToken intentionally empty
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Test Issue",
	}

	ctx := context.Background()
	err := notifier.Notify(ctx, event, target)
	if err != nil {
		t.Errorf("Notify() should silently skip, but got error: %v", err)
	}

	// Execute debouncer
	debouncer.executeAll()

	// Verify no API calls were made
	if mockClient.callCount > 0 {
		t.Errorf("Expected no Slack API calls when Slack config missing, got %d calls", mockClient.callCount)
	}
}

// TestClient_PassesBotTokenInHeader verifies that the client correctly
// includes the bot token in the Authorization header.
// This is a regression test for the "not_authed" error.
func TestClient_PassesBotTokenInHeader(t *testing.T) {
	var receivedToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Error("Authorization header missing - token not being passed!")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Extract token from "Bearer <token>"
		var token string
		fmt.Sscanf(auth, "Bearer %s", &token)
		receivedToken = token

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": true, "ts": "1234567890.123456"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL)
	expectedToken := "xoxb-test-token-abc123"

	msg := MessageBlock{Text: "Test message"}
	_, err := client.PostMessage(context.Background(), expectedToken, "#test-channel", msg)
	if err != nil {
		t.Errorf("PostMessage() error = %v", err)
	}

	if receivedToken != expectedToken {
		t.Errorf("Token not passed correctly: got %q, want %q", receivedToken, expectedToken)
	}
}

// TestClient_ReturnsErrorOnAuthFailure verifies that the client properly
// returns an error when Slack responds with "not_authed".
func TestClient_ReturnsErrorOnAuthFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok": false, "error": "not_authed"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL)
	msg := MessageBlock{Text: "Test"}

	_, err := client.PostMessage(context.Background(), "invalid-token", "#test", msg)
	if err == nil {
		t.Error("Expected error for not_authed response, got nil")
	}

	if err != nil && err.Error() != "slack api error: not_authed" {
		t.Errorf("Expected 'not_authed' error, got: %v", err)
	}
}

// tokenRecordingClient is a mock that records which token was used
type tokenRecordingClient struct {
	onPostMessage func(botToken string)
	callCount     int
}

func (m *tokenRecordingClient) PostMessage(ctx context.Context, botToken, channel string, msg MessageBlock) (string, error) {
	m.callCount++
	if m.onPostMessage != nil {
		m.onPostMessage(botToken)
	}
	return "mock-ts-123", nil
}

func (m *tokenRecordingClient) PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error {
	m.callCount++
	return nil
}

func (m *tokenRecordingClient) AddReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
	m.callCount++
	return nil
}

func (m *tokenRecordingClient) RemoveReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
	m.callCount++
	return nil
}

// TestIntegration_FullSlackNotificationFlow verifies the complete flow
// from event to Slack API call with correct token.
func TestIntegration_FullSlackNotificationFlow(t *testing.T) {
	// Track the complete flow
	var recorded struct {
		botToken  string
		channel   string
		message   string
		eventType slackfacade.EventType
	}

	mockClient := &tokenRecordingClient{
		onPostMessage: func(botToken string) {
			recorded.botToken = botToken
		},
	}

	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(mockClient, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-real-token-from-config-123",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 99,
		Title:       "Integration Test Issue",
		Body:        "This tests the full flow",
		Author:      "developer",
	}

	ctx := context.Background()
	if err := notifier.Notify(ctx, event, target); err != nil {
		t.Fatalf("Notify() failed: %v", err)
	}

	// Execute immediately
	debouncer.executeAll()

	// Verify the flow used the correct token from target
	if recorded.botToken != target.SlackBotToken {
		t.Errorf("Integration flow used wrong token: got %q, want %q",
			recorded.botToken, target.SlackBotToken)
	}

	// Verify API was actually called
	if mockClient.callCount == 0 {
		t.Error("No Slack API calls were made - flow was blocked somewhere")
	}
}
