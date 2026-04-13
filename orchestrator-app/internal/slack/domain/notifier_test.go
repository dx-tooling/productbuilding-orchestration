package domain

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
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
	m.calls = m.calls[:0]
	m.mu.Unlock()

	for _, fn := range calls {
		fn()
	}
}

type mockAssembler struct {
	snapshot *featurecontext.FeatureSnapshot
	err      error
	// Track calls for assertions
	forPRCalls    []mockForPRCall
	forIssueCalls []mockForIssueCall
	mu            sync.Mutex
}

type mockForPRCall struct {
	Owner       string
	Repo        string
	PRNumber    int
	LinkedIssue int
}

type mockForIssueCall struct {
	Owner  string
	Repo   string
	Number int
}

func (m *mockAssembler) ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*featurecontext.FeatureSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forPRCalls = append(m.forPRCalls, mockForPRCall{Owner: owner, Repo: repo, PRNumber: prNumber, LinkedIssue: linkedIssue})
	return m.snapshot, m.err
}

func (m *mockAssembler) ForIssue(ctx context.Context, owner, repo, pat string, number int) (*featurecontext.FeatureSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.forIssueCalls = append(m.forIssueCalls, mockForIssueCall{Owner: owner, Repo: repo, Number: number})
	return m.snapshot, m.err
}

func TestNotifier_Notify_NewThread(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
		URL:         "https://github.com/example-org/test-repo/issues/42",
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate existing thread
	existingThread := &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
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
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
	}
	notifier.Notify(context.Background(), event1, target)
	debouncer.executeAll()

	// Second message should post to existing thread
	event2 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	// Target without Slack config
	target := targets.TargetConfig{
		RepoOwner: "example-org",
		RepoName:  "test-repo",
		// No SlackChannel or SlackBotToken
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate existing thread so we can add reactions to it
	existingThread := &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
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
		RepoOwner:   "example-org",
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
		RepoOwner:   "example-org",
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Send multiple rapid notifications (they get buffered, not executed)
	for i := 0; i < 5; i++ {
		event := slackfacade.NotificationEvent{
			Type:        slackfacade.EventPROpened,
			RepoOwner:   "example-org",
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-existing issue thread
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 51,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	// PR #52 opened, linked to issue #51
	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "example-org",
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
	prThread, _ := repo.FindThreadByNumber(context.Background(), "example-org", "test-repo", 52)
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
	issueThread, _ := repo.FindThreadByNumber(context.Background(), "example-org", "test-repo", 51)
	if issueThread == nil || issueThread.GithubIssueID != 51 {
		t.Errorf("Original issue thread should be preserved, got %+v", issueThread)
	}
}

func TestNotifier_MultiplePRsPerIssue_AllLinkToSameThread(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-existing issue thread
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 51,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	// First PR
	event1 := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "example-org",
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
		RepoOwner:         "example-org",
		RepoName:          "test-repo",
		IssueNumber:       53,
		Title:             "Forgot password - frontend",
		LinkedIssueNumber: 51,
	}
	notifier.Notify(context.Background(), event2, target)
	debouncer.executeAll()

	// Both PRs should resolve to the issue thread
	pr52, _ := repo.FindThreadByNumber(context.Background(), "example-org", "test-repo", 52)
	pr53, _ := repo.FindThreadByNumber(context.Background(), "example-org", "test-repo", 53)

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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
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
			RepoOwner:     "example-org",
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

	// Should NOT have created a new channel-level parent message
	for _, msg := range client.postedMessages {
		if msg.Thread == "" && strings.Contains(msg.Text, "Forgot Password") {
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
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 99,
		Author:      "PrdctBldr",
		Body:        "Deploy comment",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages for comment on unknown issue, got %d", len(client.postedMessages))
	}
}

func TestNotifier_CommentOnKnownIssue_PostsToThread(t *testing.T) {
	// A comment on an issue with an existing thread should post to that thread.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "existing-thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "alice",
		Body:        "Looks good!",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(client.postedMessages))
	}
	if client.postedMessages[0].Thread != "existing-thread-ts" {
		t.Errorf("Expected reply in existing thread, got thread=%q", client.postedMessages[0].Thread)
	}
}

func TestNotifier_PRWithLinkedIssue_FindsThreadWithoutRetrySleep(t *testing.T) {
	// When a PR has a LinkedIssueNumber, the notifier should find the issue
	// thread via the linked issue check BEFORE the 5s retry sleep.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "issue-thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 53,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "issue-thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "example-org",
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

func TestNotifier_IssueClosed_WithLinkedPR_UsesForPR(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{
		snapshot: &featurecontext.FeatureSnapshot{
			PR: &featurecontext.PRState{Number: 52, Merged: true},
		},
	}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GitHubPAT:     "ghp_test",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate thread with GithubIssueID=42, GithubPRID=52
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		GithubPRID:    52,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueClosed,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	assembler.mu.Lock()
	defer assembler.mu.Unlock()

	if len(assembler.forPRCalls) != 1 {
		t.Fatalf("Expected 1 ForPR call, got %d (forIssueCalls=%d)", len(assembler.forPRCalls), len(assembler.forIssueCalls))
	}
	call := assembler.forPRCalls[0]
	if call.PRNumber != 52 {
		t.Errorf("Expected ForPR with PRNumber=52, got %d", call.PRNumber)
	}
	if call.LinkedIssue != 42 {
		t.Errorf("Expected ForPR with LinkedIssue=42, got %d", call.LinkedIssue)
	}
}

func TestNotifier_IssueClosed_WithLinkedPR_MessageShowsPR(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{
		snapshot: &featurecontext.FeatureSnapshot{
			PR: &featurecontext.PRState{Number: 52, Merged: true, Title: "Implement feature"},
		},
	}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GitHubPAT:     "ghp_test",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		GithubPRID:    52,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueClosed,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	// Should have posted a message mentioning the PR
	found := false
	for _, msg := range client.postedMessages {
		if strings.Contains(msg.Text, "merged") || strings.Contains(msg.Text, "#52") {
			found = true
			break
		}
	}
	if !found {
		var texts []string
		for _, msg := range client.postedMessages {
			texts = append(texts, msg.Text)
		}
		t.Errorf("Expected message mentioning merged PR #52, got: %v", texts)
	}
}

func TestNotifier_IssueClosed_NoLinkedPR_UsesForIssue(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{
		snapshot: &featurecontext.FeatureSnapshot{},
	}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GitHubPAT:     "ghp_test",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	// Thread with no linked PR
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "thread-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		GithubPRID:    0,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueClosed,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	assembler.mu.Lock()
	defer assembler.mu.Unlock()

	if len(assembler.forIssueCalls) != 1 {
		t.Fatalf("Expected 1 ForIssue call, got %d (forPRCalls=%d)", len(assembler.forIssueCalls), len(assembler.forPRCalls))
	}
	if len(assembler.forPRCalls) != 0 {
		t.Errorf("Expected no ForPR calls, got %d", len(assembler.forPRCalls))
	}
}

func TestNotifier_CIFailed_NoThread_SkipsNotification(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCIFailed,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages for CI event with no thread, got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
}

func TestNotifier_PreviewReady_NoThread_SkipsNotification(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages for preview event with no thread, got %d", len(client.postedMessages))
	}
}

func TestNotifier_PRMerged_NoThread_SkipsNotification(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages for merged event with no thread, got %d", len(client.postedMessages))
	}
}

func TestNotifier_IssueOpened_NoThread_StillCreatesThread(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Title:       "New issue",
		Author:      "alice",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 1 {
		t.Errorf("Expected 1 message for issue opened (creates thread), got %d", len(client.postedMessages))
	}
}

func TestMessageGenerator_ParentMessage_BodyInBlockquote(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 10,
		Title:       "Test issue",
		Author:      "alice",
		Body:        "This is the **body** with [a link](https://example.com)",
		URL:         "https://github.com/example-org/test-repo/issues/10",
	}

	msg := g.ParentMessage(event, nil)
	if !strings.Contains(msg.Text, "> ") {
		t.Errorf("Expected parent message body in blockquote, got:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "**body**") {
		t.Error("Expected bold markers to be stripped from body")
	}
	if strings.Contains(msg.Text, "[a link]") {
		t.Error("Expected markdown link to be converted to plain text")
	}
}

func TestNotifier_TwoComments_PreservedInOrder(t *testing.T) {
	// Two comments on the same issue should both be posted in arrival order.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "thread-ts-42",
		ThreadType:    "issue",
	})

	comment1 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "alice",
		Body:        "first comment",
	}
	comment2 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "bob",
		Body:        "second comment",
	}

	notifier.Notify(context.Background(), comment1, target)
	notifier.Notify(context.Background(), comment2, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 2 {
		t.Fatalf("Expected 2 posted messages, got %d", len(client.postedMessages))
	}
	if !strings.Contains(client.postedMessages[0].Text, "first comment") {
		t.Errorf("First message should contain 'first comment', got: %s", client.postedMessages[0].Text)
	}
	if !strings.Contains(client.postedMessages[1].Text, "second comment") {
		t.Errorf("Second message should contain 'second comment', got: %s", client.postedMessages[1].Text)
	}
	// Verify new format: blockquote instead of code block
	for _, msg := range client.postedMessages {
		if strings.Contains(msg.Text, "```") {
			t.Errorf("Comments should use blockquote, not code block, got: %s", msg.Text)
		}
	}
	if client.postedMessages[0].Thread != "thread-ts-42" || client.postedMessages[1].Thread != "thread-ts-42" {
		t.Errorf("Both comments should be in thread thread-ts-42")
	}
}

func TestNotifier_PROpenedPlusComment_SameBatch(t *testing.T) {
	// PR opened + comment in the same debounce window: status creates thread,
	// comment posts to it.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	prEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
		Author:      "alice",
	}
	commentEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "bot",
		Body:        "Deploy started",
	}

	notifier.Notify(context.Background(), prEvent, target)
	notifier.Notify(context.Background(), commentEvent, target)
	debouncer.executeAll()

	// Should have 2 messages: parent (from PR) + thread reply (from comment)
	if len(client.postedMessages) != 2 {
		t.Fatalf("Expected 2 posted messages, got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
	// First message is the parent (PostMessage, no thread)
	if client.postedMessages[0].Thread != "" {
		t.Errorf("First message should be a parent (no thread), got thread=%q", client.postedMessages[0].Thread)
	}
	// Second message is the comment (PostToThread)
	if client.postedMessages[1].Thread == "" {
		t.Error("Second message should be a thread reply")
	}
	if !strings.Contains(client.postedMessages[1].Text, "Deploy started") {
		t.Errorf("Thread reply should contain comment body, got: %s", client.postedMessages[1].Text)
	}
}

func TestNotifier_CommentBeforeLifecycle_SameBatch(t *testing.T) {
	// Comment arrives before PR opened in the same batch — status should
	// still be processed first (creates thread), then comment posts to it.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Comment arrives FIRST
	commentEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "bot",
		Body:        "Deploy started",
	}
	// PR opened arrives SECOND
	prEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
		Author:      "alice",
	}

	notifier.Notify(context.Background(), commentEvent, target)
	notifier.Notify(context.Background(), prEvent, target)
	debouncer.executeAll()

	// Should have 2 messages: parent (from PR, processed first) + thread reply (comment)
	if len(client.postedMessages) != 2 {
		t.Fatalf("Expected 2 posted messages, got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
	if client.postedMessages[0].Thread != "" {
		t.Errorf("First message should be a parent, got thread=%q", client.postedMessages[0].Thread)
	}
	if client.postedMessages[1].Thread == "" {
		t.Error("Second message should be a thread reply")
	}
}

func TestNotifier_StatusDedup_StillWorks(t *testing.T) {
	// Multiple status events should be deduped (only latest survives).
	// When PRReady overwrites PROpened via debounce, PRReady is a non-creation
	// event with no existing thread — it is correctly skipped by the orphan guard.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event1 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
		Author:      "alice",
	}
	event2 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add feature",
		PreviewURL:  "https://preview.example.com",
	}

	notifier.Notify(context.Background(), event1, target)
	notifier.Notify(context.Background(), event2, target)
	debouncer.executeAll()

	// PRReady overwrites PROpened; PRReady is a non-creation event with no
	// existing thread, so it is correctly skipped (no orphan threads).
	if len(client.postedMessages) != 0 {
		t.Fatalf("Expected 0 messages (PRReady without thread is skipped), got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
}

func TestNotifier_OrphanComment_RetriesGivesUp(t *testing.T) {
	// A comment on an issue with no thread ever → 0 messages after retry.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 99,
		Author:      "bot",
		Body:        "Orphan comment",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 0 {
		t.Errorf("Expected 0 messages for orphan comment, got %d", len(client.postedMessages))
	}
}

func TestNotifier_OrphanComment_RetriesFindsThread(t *testing.T) {
	// A comment arrives with no thread, but the thread is created during the retry window.
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 200 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 50,
		Author:      "bot",
		Body:        "Deploy started",
	}

	notifier.Notify(context.Background(), event, target)

	// Simulate thread being created during the retry window
	go func() {
		time.Sleep(100 * time.Millisecond)
		repo.SaveThread(context.Background(), &SlackThread{
			ID:            "late-thread-id",
			RepoOwner:     "example-org",
			RepoName:      "test-repo",
			GithubIssueID: 50,
			SlackChannel:  "#productbuilding-test",
			SlackThreadTs: "late-thread-ts",
			ThreadType:    "issue",
		})
	}()

	debouncer.executeAll()

	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message after retry found thread, got %d", len(client.postedMessages))
	}
	if client.postedMessages[0].Thread != "late-thread-ts" {
		t.Errorf("Expected reply in late-thread-ts, got thread=%q", client.postedMessages[0].Thread)
	}
}

func TestNotifier_FlushIdempotent(t *testing.T) {
	// Calling flush twice should only post once (grab-and-delete clears pending).
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "thread-ts-42",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "alice",
		Body:        "Hello",
	}

	notifier.Notify(context.Background(), event, target)

	// Manually call flush twice with the same key
	key := fmt.Sprintf("%s#%d", target.SlackChannel, event.IssueNumber)
	notifier.flush(context.Background(), key, target)
	notifier.flush(context.Background(), key, target)

	if len(client.postedMessages) != 1 {
		t.Errorf("Expected 1 message after double flush, got %d", len(client.postedMessages))
	}
}

func TestNotifier_Notify_Formatting(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	// Pre-populate thread so non-creation events have a thread to post to
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "format-test-thread",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubPRID:    42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "format-thread-ts",
		ThreadType:    "pull_request",
	})

	tests := []struct {
		name     string
		event    slackfacade.NotificationEvent
		contains string
	}{
		{
			name: "PR ready posts to existing thread",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventPRReady,
				RepoOwner:   "example-org",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Title:       "Add feature",
				PreviewURL:  "https://preview.example.com",
				UserNote:    "Test with admin/admin",
			},
			contains: "preview is live",
		},
		{
			name: "Preview failed",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventPRFailed,
				RepoOwner:   "example-org",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Status:      "compose_up",
			},
			contains: "failed during",
		},
		{
			name: "Comment with link",
			event: slackfacade.NotificationEvent{
				Type:        slackfacade.EventCommentAdded,
				RepoOwner:   "example-org",
				RepoName:    "test-repo",
				IssueNumber: 42,
				Author:      "alice",
				Body:        "This is a long comment that should be truncated",
				CommentID:   123456,
			},
			contains: "@alice commented on GitHub:",
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

func TestNotifier_NewThread_UsesMessageGenerator(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{
		snapshot: &featurecontext.FeatureSnapshot{
			Issue: &featurecontext.IssueState{Number: 42, Title: "Add dark mode", Body: "Please add dark mode.", State: "open"},
		},
	}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp_test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
		Body:        "Please add dark mode.",
		Author:      "alice",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(client.postedMessages))
	}

	msg := client.postedMessages[0].Text
	// New conversational format: blockquote body, no separators, no code blocks
	if strings.Contains(msg, "─────") {
		t.Errorf("New format should not have separators, got: %s", msg)
	}
	if strings.Contains(msg, "```") {
		t.Errorf("New format should use blockquote not code block, got: %s", msg)
	}
	if !strings.Contains(msg, "> ") {
		t.Errorf("Expected blockquote in message, got: %s", msg)
	}
}

func TestNotifier_ExistingThread_UsesMessageGenerator(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "existing-id",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "parent-ts-123",
		ThreadType:    "issue",
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Author:      "bob",
		Body:        "Great idea!",
		CommentID:   123,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(client.postedMessages))
	}

	msg := client.postedMessages[0].Text
	// New format: "@bob commented on GitHub:" with blockquote
	if !strings.Contains(msg, "@bob commented on GitHub:") {
		t.Errorf("Expected new comment format, got: %s", msg)
	}
	if !strings.Contains(msg, "> Great idea!") {
		t.Errorf("Expected body in blockquote, got: %s", msg)
	}
}

func TestNotifier_AssemblerError_FallsBackGracefully(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{
		err: fmt.Errorf("assembler error"),
	}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
		Author:      "alice",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	// Should still post (nil snapshot triggers fallback in MessageGenerator)
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message even with assembler error, got %d", len(client.postedMessages))
	}
	if client.postedMessages[0].Text == "" {
		t.Error("Message should have non-empty text even with assembler error")
	}
}

func TestNotifier_PREvent_PassesLinkedIssueToAssembler(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp_test",
	}

	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "example-org",
		RepoName:          "test-repo",
		IssueNumber:       10,
		Title:             "Fix bug",
		Author:            "alice",
		LinkedIssueNumber: 51,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	assembler.mu.Lock()
	defer assembler.mu.Unlock()
	if len(assembler.forPRCalls) == 0 {
		t.Fatal("Expected ForPR to be called")
	}
	call := assembler.forPRCalls[0]
	if call.LinkedIssue != 51 {
		t.Errorf("Expected ForPR linkedIssue=51, got %d", call.LinkedIssue)
	}
	if call.PRNumber != 10 {
		t.Errorf("Expected ForPR prNumber=10, got %d", call.PRNumber)
	}
}

func TestNotifier_IssueEvent_CallsForIssue(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	assembler := &mockAssembler{}
	notifier := NewNotifier(client, repo, debouncer, assembler)

	target := targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		SlackChannel:  "#productbuilding-test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp_test",
	}

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Bug report",
		Author:      "alice",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	assembler.mu.Lock()
	defer assembler.mu.Unlock()
	if len(assembler.forIssueCalls) == 0 {
		t.Fatal("Expected ForIssue to be called")
	}
	if len(assembler.forPRCalls) != 0 {
		t.Error("ForPR should not be called for issue events")
	}
	call := assembler.forIssueCalls[0]
	if call.Number != 42 {
		t.Errorf("Expected ForIssue number=42, got %d", call.Number)
	}
}
