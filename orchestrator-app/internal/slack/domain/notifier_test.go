package domain

import (
	"context"
	"fmt"
	"strings"
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

func (m *mockClient) PostMessage(ctx context.Context, botToken, channel string, msg MessageBlock) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts := "parent-ts-" + string(rune('a'+len(m.postedMessages)))
	m.postedMessages = append(m.postedMessages, mockPost{
		Channel: channel,
		Text:    msg.Text,
	})
	return ts, nil
}

func (m *mockClient) PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.postedMessages = append(m.postedMessages, mockPost{
		Channel: channel,
		Thread:  threadTs,
		Text:    msg.Text,
	})
	return nil
}

func (m *mockClient) AddReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reactions = append(m.reactions, mockReaction{
		Channel:   channel,
		Timestamp: timestamp,
		Emoji:     emoji,
	})
	return nil
}

func (m *mockClient) RemoveReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error {
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
	if thread.GithubIssueID > 0 {
		key := fmt.Sprintf("%s/%s#%d", thread.RepoOwner, thread.RepoName, thread.GithubIssueID)
		m.threads[key] = thread
	}
	if thread.GithubPRID > 0 {
		key := fmt.Sprintf("%s/%s#%d-pr", thread.RepoOwner, thread.RepoName, thread.GithubPRID)
		m.threads[key] = thread
	}
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

func (m *mockRepository) FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*SlackThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Search by issue ID first
	key := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, number)
	if thread, ok := m.threads[key]; ok {
		return thread, nil
	}
	// Then search by PR ID
	key = fmt.Sprintf("%s/%s#%d-pr", repoOwner, repoName, number)
	if thread, ok := m.threads[key]; ok {
		return thread, nil
	}
	return nil, nil
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

	debouncer.executeAll()

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
	debouncer.executeAll()

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
	debouncer.executeAll()

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
	debouncer.executeAll()

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
	debouncer.executeAll()

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

func TestNotifier_PRLinksToIssueThread_CreatesNewMapping(t *testing.T) {
	// When a PR references an issue via #N, the notifier should find the
	// issue's thread and create a separate PR thread mapping so future
	// events on the PR (comments, merges) land in the same thread.
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

	// Pre-existing issue thread
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubIssueID: 51,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	// PR #52 opened, linked to issue #51
	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "luminor-project",
		RepoName:          "test-repo",
		IssueNumber:       52,
		Title:             "Forgot password implemented",
		Author:            "opencode-agent[bot]",
		LinkedIssueNumber: 51,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	// Should post to the issue thread, not create a new channel message
	foundThreadReply := false
	for _, msg := range client.postedMessages {
		if msg.Thread == "issue-thread-ts" {
			foundThreadReply = true
			break
		}
	}
	if !foundThreadReply {
		t.Errorf("Expected PR notification in issue thread, got: %+v", client.postedMessages)
	}

	// Should have created a separate PR mapping (FindThreadByNumber for #52 should work)
	prThread, _ := repo.FindThreadByNumber(context.Background(), "luminor-project", "test-repo", 52)
	if prThread == nil {
		t.Fatal("Expected PR thread mapping to be created for #52")
	}
	if prThread.SlackThreadTs != "issue-thread-ts" {
		t.Errorf("PR mapping should point to issue thread, got %s", prThread.SlackThreadTs)
	}
	if prThread.GithubPRID != 52 {
		t.Errorf("PR mapping should have PR ID 52, got %d", prThread.GithubPRID)
	}

	// Original issue thread should be untouched
	issueThread, _ := repo.FindThreadByNumber(context.Background(), "luminor-project", "test-repo", 51)
	if issueThread == nil || issueThread.GithubIssueID != 51 {
		t.Errorf("Original issue thread should be preserved, got %+v", issueThread)
	}
}

func TestNotifier_MultiplePRsPerIssue_AllLinkToSameThread(t *testing.T) {
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

	// Pre-existing issue thread
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubIssueID: 51,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	// First PR
	event1 := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "luminor-project",
		RepoName:          "test-repo",
		IssueNumber:       52,
		Title:             "Forgot password - backend",
		LinkedIssueNumber: 51,
	}
	notifier.Notify(context.Background(), event1, target)
	debouncer.executeAll()

	// Second PR
	event2 := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "luminor-project",
		RepoName:          "test-repo",
		IssueNumber:       53,
		Title:             "Forgot password - frontend",
		LinkedIssueNumber: 51,
	}
	notifier.Notify(context.Background(), event2, target)
	debouncer.executeAll()

	// Both PRs should resolve to the issue thread
	pr52, _ := repo.FindThreadByNumber(context.Background(), "luminor-project", "test-repo", 52)
	pr53, _ := repo.FindThreadByNumber(context.Background(), "luminor-project", "test-repo", 53)

	if pr52 == nil || pr52.SlackThreadTs != "issue-thread-ts" {
		t.Errorf("PR #52 should map to issue thread, got %+v", pr52)
	}
	if pr53 == nil || pr53.SlackThreadTs != "issue-thread-ts" {
		t.Errorf("PR #53 should map to issue thread, got %+v", pr53)
	}
}

func TestNotifier_Flush_RetriesForNewIssue_FindsThreadMapping(t *testing.T) {
	// Simulates the race condition: agent creates issue, webhook fires,
	// but the thread mapping is saved by the handler after a delay.
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
		IssueNumber: 50,
		Title:       "Forgot Password",
		Author:      "PrdctBldr",
	}

	// Buffer the event
	notifier.Notify(context.Background(), event, target)

	// Simulate handler saving the mapping after a delay (during the retry window)
	go func() {
		time.Sleep(2 * time.Second)
		repo.SaveThread(context.Background(), &SlackThread{
			ID:            "agent-thread-id",
			RepoOwner:     "luminor-project",
			RepoName:      "test-repo",
			GithubIssueID: 50,
			SlackChannel:  "#productbuilding-test",
			SlackThreadTs: "agent-thread-ts",
			ThreadType:    "issue",
		})
	}()

	// Execute the debounced flush — it will retry and find the mapping
	debouncer.executeAll()

	// Should post to the existing thread, NOT create a new parent message
	foundThreadReply := false
	for _, msg := range client.postedMessages {
		if msg.Thread == "agent-thread-ts" {
			foundThreadReply = true
			break
		}
	}
	if !foundThreadReply {
		t.Errorf("Expected thread reply to agent-thread-ts, got messages: %+v", client.postedMessages)
	}

	// Should NOT have created a new channel-level message
	for _, msg := range client.postedMessages {
		if msg.Thread == "" && strings.Contains(msg.Text, "#50") {
			t.Errorf("Should not have created a new channel message, got: %+v", msg)
		}
	}
}

func TestNotifier_CommentOnUnknownIssue_NoNewThread(t *testing.T) {
	// A comment on an issue/PR with no existing thread should be silently
	// skipped — it must NOT create a new channel-level message.
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
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 99,
		Author:      "PrdctBldr",
		Body:        "Deploy comment",
	}

	notifier.Notify(context.Background(), event, target)
	// Comments flush immediately via goroutine; give it a moment
	time.Sleep(50 * time.Millisecond)

	// Read under lock since flush runs in a goroutine for comments
	client.mu.Lock()
	msgCount := len(client.postedMessages)
	client.mu.Unlock()

	if msgCount != 0 {
		t.Errorf("Expected no messages for comment on unknown issue, got %d", msgCount)
	}
}

func TestNotifier_CommentOnKnownIssue_PostsToThread(t *testing.T) {
	// A comment on an issue with an existing thread should post to that thread.
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

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "existing-thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "alice",
		Body:        "Looks good!",
	}

	notifier.Notify(context.Background(), event, target)
	time.Sleep(50 * time.Millisecond)

	// Read under lock since flush runs in a goroutine for comments
	client.mu.Lock()
	msgCount := len(client.postedMessages)
	var thread string
	if msgCount > 0 {
		thread = client.postedMessages[0].Thread
	}
	client.mu.Unlock()

	if msgCount != 1 {
		t.Fatalf("Expected 1 message, got %d", msgCount)
	}
	if thread != "existing-thread-ts" {
		t.Errorf("Expected reply in existing thread, got thread=%q", thread)
	}
}

func TestNotifier_PRWithLinkedIssue_FindsThreadWithoutRetrySleep(t *testing.T) {
	// When a PR has a LinkedIssueNumber, the notifier should find the issue
	// thread via the linked issue check BEFORE the 5s retry sleep.
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

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "luminor-project",
		RepoName:      "test-repo",
		GithubIssueID: 53,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "luminor-project",
		RepoName:          "test-repo",
		IssueNumber:       54,
		Title:             "Implement feature",
		Author:            "opencode-agent[bot]",
		LinkedIssueNumber: 53,
	}

	notifier.Notify(context.Background(), event, target)

	// Measure how long flush takes — it should NOT include the 5s retry sleep
	start := time.Now()
	debouncer.executeAll()
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("flush took %v — linked issue lookup should skip the 5s retry sleep", elapsed)
	}

	// Should have posted to the issue thread
	foundThreadReply := false
	for _, msg := range client.postedMessages {
		if msg.Thread == "issue-thread-ts" {
			foundThreadReply = true
			break
		}
	}
	if !foundThreadReply {
		t.Errorf("Expected PR notification in issue thread, got: %+v", client.postedMessages)
	}
}

func TestSanitizeForCodeBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "strips HTML tags",
			input: "<p>Hello <b>world</b></p>",
			want:  "Hello world",
		},
		{
			name:  "converts markdown images to alt text",
			input: "See ![screenshot](https://img.example.com/shot.png) here",
			want:  "See screenshot here",
		},
		{
			name:  "converts markdown links to text",
			input: "Check [this PR](https://github.com/foo/bar/pull/1) out",
			want:  "Check this PR out",
		},
		{
			name:  "strips heading markers",
			input: "### Summary\nSome text\n## Details\nMore text",
			want:  "Summary\nSome text\nDetails\nMore text",
		},
		{
			name:  "strips bold markers",
			input: "This is **important** stuff",
			want:  "This is important stuff",
		},
		{
			name:  "removes triple backticks",
			input: "```go\nfmt.Println(\"hi\")\n```",
			want:  "fmt.Println(\"hi\")",
		},
		{
			name:  "collapses excessive newlines",
			input: "line1\n\n\n\n\nline2",
			want:  "line1\n\nline2",
		},
		{
			name:  "replaces HTML entities",
			input: "opencode session&nbsp;&nbsp;|&nbsp;&nbsp;github run &amp; deploy &lt;v2&gt;",
			want:  "opencode session  |  github run & deploy <v2>",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeForCodeBlock(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeForCodeBlock() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatParentMessage_BodyInCodeBlock(t *testing.T) {
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "luminor-project",
		RepoName:    "test-repo",
		IssueNumber: 10,
		Title:       "Test issue",
		Author:      "alice",
		Body:        "This is the **body** with [a link](https://example.com)",
		URL:         "https://github.com/luminor-project/test-repo/issues/10",
	}

	msg := formatParentMessage(event)
	if !strings.Contains(msg.Text, "```") {
		t.Errorf("Expected parent message body to be wrapped in code block, got:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "**body**") {
		t.Error("Expected bold markers to be stripped from body")
	}
	if strings.Contains(msg.Text, "[a link]") {
		t.Error("Expected markdown link to be converted to plain text")
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
			name: "PR ready creates parent (new thread)",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventPRReady,
				RepoOwner:   "luminor-project",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Title:       "Add feature",
				PreviewURL:  "https://preview.example.com",
				UserNote:    "Test with admin/admin",
			},
			contains: "*Pull Request #42* — Add feature",
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
			contains: "─────\n*Preview failed*",
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
			contains: "─────\n*@alice* commented:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear previous messages
			client.postedMessages = nil

			notifier.Notify(context.Background(), tt.event, target)
			debouncer.executeAll()

			if len(client.postedMessages) == 0 {
				t.Fatal("Expected at least 1 message")
			}

			msg := client.postedMessages[len(client.postedMessages)-1].Text
			if msg == "" {
				t.Error("Message text should not be empty")
			}
			if !strings.Contains(msg, tt.contains) {
				t.Errorf("Expected message to contain %q, got:\n%s", tt.contains, msg)
			}
		})
	}
}
