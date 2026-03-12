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

// TestSlackThread_UsesCorrectThreadType verifies that the thread type
// is stored in the database-compatible format ('issue' or 'pull_request'),
// not the human-readable format ('Issue' or 'Pull Request').
// This is a regression test for the threading bug where all messages
// created new parent posts instead of threading, caused by CHECK constraint
// failures when saving threads to the database.
func TestSlackThread_UsesCorrectThreadType(t *testing.T) {
	// Issue event should return 'issue'
	issueEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Test Issue",
	}

	if issueEvent.ThreadType() != "issue" {
		t.Errorf("Issue ThreadType() = %q, want 'issue'", issueEvent.ThreadType())
	}

	// PR event should return 'pull_request'
	prEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 99,
		Title:       "Test PR",
	}

	if prEvent.ThreadType() != "pull_request" {
		t.Errorf("PR ThreadType() = %q, want 'pull_request'", prEvent.ThreadType())
	}

	// Also verify IssueOrPR returns human-readable format (different use case)
	if issueEvent.IssueOrPR() != "Issue" {
		t.Errorf("IssueOrPR() = %q, want 'Issue'", issueEvent.IssueOrPR())
	}
	if prEvent.IssueOrPR() != "Pull Request" {
		t.Errorf("IssueOrPR() = %q, want 'Pull Request'", prEvent.IssueOrPR())
	}
}

// TestNotifier_PRFromIssue_UsesSameThread verifies that when a PR is created
// from an existing issue (which share the same number in GitHub), both use
// the SAME Slack thread instead of creating duplicate parent messages.
//
// This is a critical regression test. In GitHub, PRs are also issues and
// share the same numbering. When you create an issue and then create a PR
// from it (e.g., via /opencode command), they have the same number but the
// system must thread them together.
//
// Scenario:
//  1. Issue #42 created → creates Slack thread #42
//  2. PR #42 created (from Issue #42) → should use thread #42, NOT create new
//  3. Comments on PR → should go to existing thread #42
//
// Without this fix: Two separate parent messages appear in Slack
// With this fix: One unified thread with all activity
func TestNotifier_PRFromIssue_UsesSameThread(t *testing.T) {
	// Track unique thread creations (by thread ID, not saves)
	threadIDs := make(map[string]bool)
	repo := newMockRepository()

	// Wrap the repository to track unique thread creations
	trackingRepo := &trackingThreadRepository{
		mockRepository: repo,
		onSave: func(threadID string) {
			threadIDs[threadID] = true
		},
	}

	client := &tokenRecordingClient{}
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, trackingRepo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "test-owner",
		RepoName:      "test-repo",
		SlackChannel:  "#test-channel",
		SlackBotToken: "xoxb-test",
	}

	// Step 1: Create Issue #42
	issueEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature X",
		Body:        "We need feature X for better UX",
		Author:      "developer",
	}

	ctx := context.Background()
	if err := notifier.Notify(ctx, issueEvent, target); err != nil {
		t.Fatalf("Failed to notify for issue: %v", err)
	}
	debouncer.executeAll()

	// Verify thread was created
	if len(threadIDs) != 1 {
		t.Fatalf("Expected 1 thread after issue creation, got %d", len(threadIDs))
	}

	// Verify thread was saved with issue ID
	thread, err := repo.FindThread(ctx, "test-owner", "test-repo", 42)
	if err != nil {
		t.Fatalf("Failed to find issue thread: %v", err)
	}
	if thread == nil {
		t.Fatal("Issue thread was not saved to repository")
	}
	if thread.GithubIssueID != 42 {
		t.Errorf("Thread has wrong issue ID: got %d, want 42", thread.GithubIssueID)
	}
	if thread.GithubPRID != 0 {
		t.Errorf("Thread should have no PR ID yet: got %d", thread.GithubPRID)
	}

	// Step 2: Create PR #42 (same number as the issue - this is the key test!)
	// In GitHub, PRs are also issues and share the numbering
	prEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42, // Same number!
		Title:       "Add feature X",
		Body:        "Implementation of feature X",
		Author:      "developer",
	}

	if err := notifier.Notify(ctx, prEvent, target); err != nil {
		t.Fatalf("Failed to notify for PR: %v", err)
	}
	debouncer.executeAll()

	// CRITICAL: Should NOT create a second thread
	// This was the bug - it would create duplicate parent messages
	if len(threadIDs) != 1 {
		t.Errorf("REGRESSION BUG: Expected 1 thread total (issue and PR should share), got %d\n"+
			"This means the PR created a separate thread instead of using the issue's thread.", len(threadIDs))
	}

	// Verify the thread was updated to track both issue and PR
	updatedThread, err := repo.FindThread(ctx, "test-owner", "test-repo", 42)
	if err != nil {
		t.Fatalf("Failed to find updated thread: %v", err)
	}
	if updatedThread == nil {
		t.Fatal("Thread disappeared after PR creation")
	}

	// Thread should now track BOTH IDs
	if updatedThread.GithubIssueID != 42 {
		t.Errorf("Thread lost issue ID: got %d, want 42", updatedThread.GithubIssueID)
	}
	if updatedThread.GithubPRID != 42 {
		t.Errorf("Thread should have PR ID added: got %d, want 42\n"+
			"The PR notification should have updated the existing thread", updatedThread.GithubPRID)
	}
	if updatedThread.ThreadType != "pull_request" {
		t.Errorf("Thread type should be updated to 'pull_request': got %q", updatedThread.ThreadType)
	}

	// Step 3: Add comment to PR - should go to existing thread
	commentEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature X",
		Body:        "This looks great!",
		Author:      "reviewer",
	}

	if err := notifier.Notify(ctx, commentEvent, target); err != nil {
		t.Fatalf("Failed to notify for comment: %v", err)
	}
	debouncer.executeAll()

	// Should still only be 1 thread
	if len(threadIDs) != 1 {
		t.Errorf("Comment created a new thread! Expected 1, got %d", len(threadIDs))
	}

	t.Log("✅ PASS: Issue and PR correctly share the same Slack thread")
	t.Log("   - Issue #42 created thread")
	t.Log("   - PR #42 used same thread (no duplicate)")
	t.Log("   - Comment went to existing thread")
	t.Log("   - Thread correctly tracks both issue and PR IDs")
}

// trackingThreadRepository wraps a mockRepository to track unique thread creations
type trackingThreadRepository struct {
	*mockRepository
	onSave func(threadID string)
}

func (c *trackingThreadRepository) SaveThread(ctx context.Context, thread *SlackThread) error {
	c.onSave(thread.ID)
	return c.mockRepository.SaveThread(ctx, thread)
}

// TestNotifier_HandlesThreadNotFoundError verifies that the notifier correctly
// handles the "thread not found" error from the repository and still finds
// existing threads via fallback lookups.
//
// This is a regression test for the bug where all messages created new parent
// posts instead of threading, because the code checked `err == nil` but the
// repository returns "thread not found" as an error when a thread doesn't exist.
//
// Bug scenario:
//  1. Issue #5 created → Thread #5 saved to DB
//  2. Comment on Issue #5 → Repository.FindThread returns: nil, "thread not found"
//  3. Old code checked: thread == nil && err == nil → FALSE (err was "not found")
//  4. Code skipped fallback lookup → Created NEW thread → UNIQUE constraint violation
//  5. Comment appeared as separate parent message in Slack
//
// Fixed behavior:
//  1. Issue #5 created → Thread #5 saved to DB
//  2. Comment on Issue #5 → Repository.FindThread returns: nil, "thread not found"
//  3. New code checks: thread == nil → TRUE (ignores the error)
//  4. Code tries fallback lookup → Finds thread by PR number → Posts to existing thread
//  5. Comment correctly appears as reply in existing thread
func TestNotifier_HandlesThreadNotFoundError(t *testing.T) {
	// Repository that returns "not found" error (like the real SQLite repo)
	notFoundRepo := &notFoundErrorRepository{
		mockRepo: newMockRepository(),
	}

	client := &mockClient{}
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, notFoundRepo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "test-owner",
		RepoName:      "test-repo",
		SlackChannel:  "#test-channel",
		SlackBotToken: "xoxb-test",
	}

	ctx := context.Background()

	// Step 1: Create Issue #5
	issueEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 5,
		Title:       "Test Issue",
		Author:      "developer",
	}

	if err := notifier.Notify(ctx, issueEvent, target); err != nil {
		t.Fatalf("Failed to create issue: %v", err)
	}
	debouncer.executeAll()

	// Verify thread was created (1 parent message)
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message after issue creation, got %d", len(client.postedMessages))
	}

	// Step 2: Add comment to Issue #5
	// The repository will return "thread not found" error for this lookup
	// This is where the bug occurred - the code must handle this correctly
	commentEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "test-owner",
		RepoName:    "test-repo",
		IssueNumber: 5,
		Title:       "Test Issue",
		Body:        "This is a comment",
		Author:      "reviewer",
	}

	if err := notifier.Notify(ctx, commentEvent, target); err != nil {
		t.Fatalf("Failed to add comment: %v", err)
	}
	debouncer.executeAll()

	// CRITICAL: Should NOT create a new parent message
	// The bug would cause 3 messages (2 parents + 1 reply)
	// Fixed: Should be 2 messages (1 parent + 1 reply)
	if len(client.postedMessages) != 2 {
		t.Errorf("BUG REGRESSION: Expected 2 messages total (1 parent + 1 reply), got %d\n"+
			"If >2, the comment created a new thread instead of replying to existing.", len(client.postedMessages))
	}

	// Verify the second message is a reply (has thread field set)
	if len(client.postedMessages) >= 2 {
		reply := client.postedMessages[1]
		// Reply should have Thread field set (indicating it's a reply, not parent)
		if reply.Thread == "" {
			t.Errorf("Comment appears to be a new parent message (Thread=''), not a reply\n" +
				"The comment should have been threaded under the parent.")
		}
	}

	t.Log("✅ PASS: 'thread not found' error handled correctly")
	t.Log("   - Issue created parent message")
	t.Log("   - Repository returned 'not found' error on comment lookup")
	t.Log("   - Code correctly ignored error and found existing thread")
	t.Log("   - Comment posted as reply (no duplicate thread)")
}

// TestNotifier_PRWithDifferentNumber_ThreadsToLinkedIssue verifies that when
// a PR has a different number than the issue it was created from, the PR
// notification is posted as a reply in the existing issue's thread.
//
// This is the real-world scenario: Issue #16 is created, then someone runs
// /opencode which creates PR #17. GitHub assigns different sequential numbers
// to issues and PRs. The PR body contains "Fixes #16" which links them.
//
// Bug scenario (before fix):
//  1. Issue #16 created → Thread saved with GithubIssueID=16
//  2. PR #17 opened (body: "Fixes #16") → FindThreadByNumber(17) → NOT FOUND
//  3. Creates new parent message for PR #17 → Duplicate thread in Slack
//
// Fixed behavior:
//  1. Issue #16 created → Thread saved with GithubIssueID=16
//  2. PR #17 opened (LinkedIssueNumber=16) → FindThreadByNumber(17) → not found
//  3. Falls back to FindThreadByNumber(16) → FOUND → Posts reply to issue #16's thread
func TestNotifier_PRWithDifferentNumber_ThreadsToLinkedIssue(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "playground",
		SlackChannel:  "#productbuilding",
		SlackBotToken: "xoxb-test",
	}

	ctx := context.Background()

	// Step 1: Issue #16 is created
	issueEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "playground",
		IssueNumber: 16,
		Title:       "Explain technical details on homepage",
		Body:        "Please add tech info",
		Author:      "manuelkiessling",
	}

	if err := notifier.Notify(ctx, issueEvent, target); err != nil {
		t.Fatalf("Failed to notify for issue: %v", err)
	}
	debouncer.executeAll()

	// Verify: 1 parent message posted
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message after issue creation, got %d", len(client.postedMessages))
	}
	if client.postedMessages[0].Thread != "" {
		t.Fatal("First message should be a parent (no thread), but it's a reply")
	}

	// Step 2: PR #17 is opened, linked to issue #16 via "Fixes #16" in body
	prEvent := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "luminor-project",
		RepoName:          "playground",
		IssueNumber:       17, // Different number than the issue!
		Title:             "Added tech/arch section to homepage",
		Body:              "Fixes #16\n\nAdded technical architecture section",
		Author:            "opencode-agent[bot]",
		LinkedIssueNumber: 16, // Extracted from "Fixes #16" in PR body
	}

	if err := notifier.Notify(ctx, prEvent, target); err != nil {
		t.Fatalf("Failed to notify for PR: %v", err)
	}
	debouncer.executeAll()

	// CRITICAL: Should be 2 messages total (1 parent + 1 thread reply)
	// NOT 2 parent messages (which is the bug)
	if len(client.postedMessages) != 2 {
		t.Fatalf("Expected 2 messages total (1 parent + 1 reply), got %d", len(client.postedMessages))
	}

	// The second message MUST be a thread reply, not a new parent
	prMsg := client.postedMessages[1]
	if prMsg.Thread == "" {
		t.Errorf("REGRESSION BUG: PR #17 created a new parent message instead of threading to issue #16's thread.\n"+
			"Expected Thread to be set (reply), but got empty string (parent).\n"+
			"The PR should have used LinkedIssueNumber=%d to find the existing thread.", prEvent.LinkedIssueNumber)
	}

	// Step 3: Comment on PR #17 should also go to the same thread.
	// NOTE: This comment does NOT have LinkedIssueNumber set — it's a plain
	// PR comment. It should still thread correctly because Step 2 saved
	// the PR #17 → thread mapping (GithubPRID=17).
	commentEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "luminor-project",
		RepoName:    "playground",
		IssueNumber: 17,
		Body:        "LGTM!",
		Author:      "manuelkiessling",
	}

	if err := notifier.Notify(ctx, commentEvent, target); err != nil {
		t.Fatalf("Failed to notify for comment: %v", err)
	}
	debouncer.executeAll()

	// Should be 3 messages total: 1 parent + 2 replies
	if len(client.postedMessages) != 3 {
		t.Errorf("Expected 3 messages total, got %d", len(client.postedMessages))
	}

	// All replies should be in the same thread
	if len(client.postedMessages) >= 3 {
		commentMsg := client.postedMessages[2]
		if commentMsg.Thread == "" {
			t.Error("Comment on PR #17 should be threaded, but was posted as parent")
		}
		if commentMsg.Thread != prMsg.Thread {
			t.Errorf("Comment and PR should be in the same thread: PR thread=%q, comment thread=%q",
				prMsg.Thread, commentMsg.Thread)
		}
	}
}

// notFoundErrorRepository wraps a mockRepository and returns "not found" errors
// like the real SQLite repository does when a thread doesn't exist
type notFoundErrorRepository struct {
	mockRepo *mockRepository
}

func (n *notFoundErrorRepository) SaveThread(ctx context.Context, thread *SlackThread) error {
	return n.mockRepo.SaveThread(ctx, thread)
}

func (n *notFoundErrorRepository) FindThread(ctx context.Context, repoOwner, repoName string, issueNumber int) (*SlackThread, error) {
	thread, _ := n.mockRepo.FindThread(ctx, repoOwner, repoName, issueNumber)
	if thread == nil {
		// Return "not found" error like real SQLite repository
		return nil, fmt.Errorf("thread not found")
	}
	return thread, nil
}

func (n *notFoundErrorRepository) FindThreadByPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*SlackThread, error) {
	thread, _ := n.mockRepo.FindThreadByPR(ctx, repoOwner, repoName, prNumber)
	if thread == nil {
		// Return "not found" error like real SQLite repository
		return nil, fmt.Errorf("thread not found")
	}
	return thread, nil
}

func (n *notFoundErrorRepository) FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*SlackThread, error) {
	thread, _ := n.mockRepo.FindThreadByNumber(ctx, repoOwner, repoName, number)
	if thread == nil {
		// Return "not found" error like real SQLite repository
		return nil, fmt.Errorf("thread not found")
	}
	return thread, nil
}
