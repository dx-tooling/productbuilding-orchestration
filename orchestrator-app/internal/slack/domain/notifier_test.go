package domain

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// Mock implementations for testing
type mockClient struct {
	postedMessages []mockPost
	reactions      []mockReaction
	mu             sync.Mutex
}

type mockPost struct {
	Channel string
	Thread  string // empty for parent, set for thread reply
	Text    string
}

type mockReaction struct {
	Channel   string
	Timestamp string
	Emoji     string
}

func (m *mockClient) PostMessage(ctx context.Context, channel string, msg MessageBlock) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts := "parent-ts-" + string(rune('a'+len(m.postedMessages)))
	m.postedMessages = append(m.postedMessages, mockPost{
		Channel: channel,
		Text:    msg.Text,
	})
	return ts, nil
}

func (m *mockClient) PostToThread(ctx context.Context, channel, threadTs string, msg MessageBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postedMessages = append(m.postedMessages, mockPost{
		Channel: channel,
		Thread:  threadTs,
		Text:    msg.Text,
	})
	return nil
}

func (m *mockClient) AddReaction(ctx context.Context, channel, timestamp, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions = append(m.reactions, mockReaction{
		Channel:   channel,
		Timestamp: timestamp,
		Emoji:     emoji,
	})
	return nil
}

func (m *mockClient) RemoveReaction(ctx context.Context, channel, timestamp, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Find and mark as removed (simplified)
	for i, r := range m.reactions {
		if r.Channel == channel && r.Timestamp == timestamp && r.Emoji == emoji {
			m.reactions = append(m.reactions[:i], m.reactions[i+1:]...)
			break
		}
	}
	return nil
}

type mockRepository struct {
	threads map[string]*SlackThread
	mu      sync.Mutex
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		threads: make(map[string]*SlackThread),
	}
}

func (m *mockRepository) SaveThread(ctx context.Context, thread *SlackThread) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s#%d", thread.RepoOwner, thread.RepoName, thread.GithubIssueID)
	m.threads[key] = thread
	return nil
}

func (m *mockRepository) FindThread(ctx context.Context, repoOwner, repoName string, issueNumber int) (*SlackThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, issueNumber)
	thread, ok := m.threads[key]
	if !ok {
		return nil, nil // Simulate not found
	}
	return thread, nil
}

func (m *mockRepository) FindThreadByPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*SlackThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf("%s/%s#%d-pr", repoOwner, repoName, prNumber)
	thread, ok := m.threads[key]
	if !ok {
		return nil, nil
	}
	return thread, nil
}

type mockDebouncer struct {
	calls []struct {
		key  string
		wait time.Duration
		fn   func()
	}
	mu sync.Mutex
}

func newMockDebouncer() *mockDebouncer {
	return &mockDebouncer{
		calls: make([]struct {
			key  string
			wait time.Duration
			fn   func()
		}, 0),
	}
}

func (m *mockDebouncer) Debounce(key string, wait time.Duration, fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, struct {
		key  string
		wait time.Duration
		fn   func()
	}{key, wait, fn})
	// Execute immediately for testing (simulating debounce expiration)
	go fn()
}

func (m *mockDebouncer) executeAll() {
	m.mu.Lock()
	calls := make([]func(), len(m.calls))
	for i, c := range m.calls {
		calls[i] = c.fn
	}
	m.mu.Unlock()

	for _, fn := range calls {
		fn()
	}
}

func TestNotifier_Notify_NewThread(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
		URL:         "https://github.com/luminor-project/test-repo/issues/42",
		Author:      "alice",
	}

	ctx := context.Background()
	err := notifier.Notify(ctx, event, target)
	if err != nil {
		t.Errorf("Notify() error = %v", err)
	}

	// Wait for debounce
	time.Sleep(100 * time.Millisecond)

	// Should create parent message
	if len(client.postedMessages) != 1 {
		t.Errorf("Expected 1 posted message, got %d", len(client.postedMessages))
	}

	msg := client.postedMessages[0]
	if msg.Channel != "#productbuilding-test" {
		t.Errorf("Expected channel #productbuilding-test, got %s", msg.Channel)
	}
	if msg.Text == "" {
		t.Error("Expected non-empty message text")
	}
}

func TestNotifier_Notify_ExistingThread(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate existing thread
	existingThread := &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "parent-ts-123",
		ThreadType:    "issue",
	}
	repo.SaveThread(context.Background(), existingThread)

	// First message creates parent
	event1 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
	}
	notifier.Notify(context.Background(), event1, target)
	time.Sleep(100 * time.Millisecond)

	// Second message should post to existing thread
	event2 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "bob",
		Body:        "Great idea!",
	}
	notifier.Notify(context.Background(), event2, target)
	time.Sleep(100 * time.Millisecond)

	// Should have 2 messages: parent + reply
	if len(client.postedMessages) != 2 {
		t.Errorf("Expected 2 posted messages, got %d", len(client.postedMessages))
	}

	// Second message should be in thread
	if client.postedMessages[1].Thread != "parent-ts-123" {
		t.Errorf("Expected thread reply to parent-ts-123, got %s", client.postedMessages[1].Thread)
	}
}

func TestNotifier_Notify_NoSlackConfig(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	// Target without Slack config
	target := targets.TargetConfig{
		RepoOwner: "luminor-project",
		RepoName:  "test-repo",
		// No SlackChannel or SlackBotToken
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
	}

	err := notifier.Notify(context.Background(), event, target)
	if err != nil {
		t.Errorf("Notify() should silently skip when no Slack config, got error: %v", err)
	}

	// Should not post anything
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected 0 messages when no Slack config, got %d", len(client.postedMessages))
	}
}

func TestNotifier_Notify_EmojiReaction(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate existing thread so we can add reactions to it
	existingThread := &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubPRID:    42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "parent-ts-123",
		ThreadType:    "pull_request",
	}
	repo.SaveThread(context.Background(), existingThread)

	// First create the thread with PR opened
	event1 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
	}
	notifier.Notify(context.Background(), event1, target)
	time.Sleep(100 * time.Millisecond)

	// Now send PR ready with emoji reaction (thread exists now)
	event2 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		Emoji:       "white_check_mark",
		ThreadTs:    "parent-ts-123",
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
		PreviewURL:  "https://preview.example.com",
	}

	notifier.Notify(context.Background(), event2, target)
	time.Sleep(100 * time.Millisecond)

	// Should have added emoji reaction
	found := false
	for _, r := range client.reactions {
		if r.Emoji == "white_check_mark" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected white_check_mark reaction to be added")
	}
}

func TestNotifier_Notify_Debouncing(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Send multiple rapid notifications (they get buffered, not executed)
	for i := 0; i < 5; i++ {
		event := slackfacade.NotificationEvent{
			Type:        slackfacade.EventPROpened,
			RepoOwner:   "luminor-project",
			RepoName:    "test-repo",
			IssueNumber: 42,
			Title:       "Add feature",
		}
		notifier.Notify(context.Background(), event, target)
	}

	// At this point, messages should not be sent yet (debounced)
	// But our mock executes immediately, so we have 5 calls queued
	// Execute all debounced calls
	debouncer.executeAll()

	// Should only post once due to debouncing (only the last event is kept)
	if len(client.postedMessages) != 1 {
		t.Errorf("Expected 1 message after debouncing 5 rapid calls, got %d", len(client.postedMessages))
	}
}

func TestNotifier_Notify_Formatting(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer)

	target := targets.TargetConfig{
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	tests := []struct {
		name     string
		event    slackfacade.NotificationEvent
		contains string
	}{
		{
			name: "PR ready with user note",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventPRReady,
				RepoOwner:   "luminor-project",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Title:       "Add feature",
				PreviewURL:  "https://preview.example.com",
				UserNote:    "Test with admin/admin",
			},
			contains: "admin/admin",
		},
		{
			name: "Preview failed",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventPRFailed,
				RepoOwner:   "luminor-project",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Status:      "compose_up",
			},
			contains: "Failed",
		},
		{
			name: "Comment with link",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventCommentAdded,
				RepoOwner:   "luminor-project",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Author:      "alice",
				Body:        "This is a long comment that should be truncated",
				CommentID:   123456,
			},
			contains: "alice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear previous messages
			client.postedMessages = nil

			notifier.Notify(context.Background(), tt.event, target)
			time.Sleep(100 * time.Millisecond)

			if len(client.postedMessages) == 0 {
				t.Fatal("Expected at least 1 message")
			}

			msg := client.postedMessages[0].Text
			if msg == "" {
				t.Error("Message text should not be empty")
			}
		})
	}
}
