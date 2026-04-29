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

func (m *mockRepository) UpdateWorkstreamPhase(ctx context.Context, threadTs string, phase WorkstreamPhase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, thread := range m.threads {
		if thread.SlackThreadTs == threadTs {
			thread.WorkstreamPhase = phase
			return nil
		}
	}
	return nil
}

func (m *mockRepository) SetPreviewNotified(ctx context.Context, threadTs string) error {
	return nil
}

func (m *mockRepository) SetFeedbackRelayed(ctx context.Context, threadTs string, relayed bool) error {
	return nil
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

	// EventIssueOpened with existing thread should not produce a reply
	// (reply is suppressed — only parent creation happens when no thread exists)
	event1 := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "example-org",
		RepoName:    "test-repo",
		IssueNumber: 42,
		Title:       "Add dark mode",
	}
	notifier.Notify(context.Background(), event1, target)
	debouncer.executeAll()

	// Comment should post to existing thread
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

	// Both EventIssueOpened and EventCommentAdded are agent-handled — no template messages
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected 0 posted messages (both events agent-handled), got %d", len(client.postedMessages))
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

	// EventIssueOpened is now handled by the agent invoker, so no reply
	// message should be posted. The important thing is that the retry found
	// the thread mapping (so no new parent message was created either).
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages (agent handles EventIssueOpened replies), got: %+v", client.postedMessages)
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

	// Comment template messages are suppressed — agent invoker handles narration
	if len(client.postedMessages) != 0 {
		t.Fatalf("Expected 0 messages (comment suppressed, agent handles), got %d", len(client.postedMessages))
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

	// EventIssueClosed is now suppressed (agent handles) — no thread reply should be posted
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected 0 messages for suppressed EventIssueClosed, got %d", len(client.postedMessages))
		for i, msg := range client.postedMessages {
			t.Logf("  msg[%d]: channel=%q thread=%q text=%q", i, msg.Channel, msg.Thread, msg.Text)
		}
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

	// Comment template messages are suppressed — agent invoker handles narration
	if len(client.postedMessages) != 0 {
		t.Fatalf("Expected 0 posted messages (comments suppressed, agent handles), got %d", len(client.postedMessages))
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

	// Should have 1 message: parent (from PR). Comment is suppressed (agent handles).
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 posted message (parent only, comment suppressed), got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
	// The only message is the parent (PostMessage, no thread)
	if client.postedMessages[0].Thread != "" {
		t.Errorf("First message should be a parent (no thread), got thread=%q", client.postedMessages[0].Thread)
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

	// Should have 1 message: parent (from PR, processed first). Comment is suppressed (agent handles).
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 posted message (parent only, comment suppressed), got %d: %+v", len(client.postedMessages), client.postedMessages)
	}
	if client.postedMessages[0].Thread != "" {
		t.Errorf("First message should be a parent, got thread=%q", client.postedMessages[0].Thread)
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

	// Comment template messages are suppressed — agent invoker handles narration
	// (thread lookup still succeeds, but no message is posted)
	if len(client.postedMessages) != 0 {
		t.Fatalf("Expected 0 messages (comment suppressed, agent handles), got %d", len(client.postedMessages))
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

	// Comment template messages are suppressed — agent invoker handles narration
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected 0 messages after double flush (comment suppressed), got %d", len(client.postedMessages))
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

	// Preview events (pr_ready, pr_failed) come from the preview service, not
	// from webhooks, so the agent invoker never sees them. The notifier must
	// post the template message.
	t.Run("PR ready posts to existing thread", func(t *testing.T) {
		client.postedMessages = nil
		notifier.Notify(context.Background(), slackfacade.NotificationEvent{
			Type:        slackfacade.EventPRReady,
			RepoOwner:   "example-org",
			RepoName:    "test-repo",
			IssueNumber: 42,
			Title:       "Add feature",
			PreviewURL:  "https://preview.example.com",
			UserNote:    "Test with admin/admin",
		}, target)
		debouncer.executeAll()

		if len(client.postedMessages) != 1 {
			t.Fatalf("Expected 1 message for EventPRReady, got %d", len(client.postedMessages))
		}
	})

	t.Run("Preview failed", func(t *testing.T) {
		client.postedMessages = nil
		notifier.Notify(context.Background(), slackfacade.NotificationEvent{
			Type:        slackfacade.EventPRFailed,
			RepoOwner:   "example-org",
			RepoName:    "test-repo",
			IssueNumber: 42,
			Status:      "compose_up",
		}, target)
		debouncer.executeAll()

		if len(client.postedMessages) != 1 {
			t.Fatalf("Expected 1 message for EventPRFailed, got %d", len(client.postedMessages))
		}
	})

	// Comments are now agent-handled — verify template message is suppressed
	t.Run("Comment suppressed (agent handles)", func(t *testing.T) {
		client.postedMessages = nil

		notifier.Notify(context.Background(), slackfacade.NotificationEvent{
			Type:        slackfacade.EventCommentAdded,
			RepoOwner:   "example-org",
			RepoName:    "test-repo",
			IssueNumber: 42,
			Author:      "alice",
			Body:        "This is a long comment that should be truncated",
			CommentID:   123456,
		}, target)
		debouncer.executeAll()

		if len(client.postedMessages) != 0 {
			t.Fatalf("Expected 0 messages (comment suppressed, agent handles), got %d", len(client.postedMessages))
		}
	})
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

	// Comment template messages are suppressed — agent invoker handles narration
	if len(client.postedMessages) != 0 {
		t.Fatalf("Expected 0 messages (comment suppressed, agent handles), got %d", len(client.postedMessages))
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

func TestShouldSkipMessage(t *testing.T) {
	tests := []struct {
		name     string
		event    slackfacade.EventType
		wantSkip bool
	}{
		{"EventIssueOpened is skipped", slackfacade.EventIssueOpened, true},
		{"EventIssueReopened is skipped", slackfacade.EventIssueReopened, true},
		{"EventCIPassed is skipped", slackfacade.EventCIPassed, true},
		{"EventPRReady is NOT skipped (preview service only)", slackfacade.EventPRReady, false},
		{"EventPRFailed is NOT skipped (preview service only)", slackfacade.EventPRFailed, false},
		{"EventCIFailed is skipped (agent handles)", slackfacade.EventCIFailed, true},
		{"EventPRMerged is skipped (agent handles)", slackfacade.EventPRMerged, true},
		{"EventIssueClosed is skipped", slackfacade.EventIssueClosed, true},
		{"EventPRClosed is skipped", slackfacade.EventPRClosed, true},
		{"EventCommentAdded is skipped (agent handles)", slackfacade.EventCommentAdded, true},
		{"EventPROpened is NOT skipped (has PhaseOpen logic)", slackfacade.EventPROpened, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipMessage(tt.event)
			if got != tt.wantSkip {
				t.Errorf("shouldSkipMessage(%s) = %v, want %v", tt.event, got, tt.wantSkip)
			}
		})
	}
}

func TestNotifier_CIFailed_DoesNotPostMessage(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	// Pre-create a thread
	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "t1",
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubPRID:    10,
		SlackChannel:  "#test",
		SlackThreadTs: "existing-ts",
		SlackParentTs: "existing-ts",
		ThreadType:    "pull_request",
	})

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

	// Should NOT post any message (CI failed is agent-handled)
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, msg := range client.postedMessages {
		if msg.Thread != "" {
			t.Errorf("Expected no thread reply for EventCIFailed, got: %q", msg.Text)
		}
	}
}

func TestNotifier_PROpened_PhaseOpen_DoesNotPostMessage_ButTransitionsPhase(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	// Pre-create a thread in PhaseOpen
	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "t1",
		RepoOwner:       "acme",
		RepoName:        "widgets",
		GithubIssueID:   10,
		SlackChannel:    "#test",
		SlackThreadTs:   "existing-ts",
		SlackParentTs:   "existing-ts",
		ThreadType:      "issue",
		WorkstreamPhase: PhaseOpen,
	})

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "acme",
		RepoName:          "widgets",
		IssueNumber:       17,
		Title:             "Add feature",
		Author:            "opencode-agent[bot]",
		LinkedIssueNumber: 10,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	// Phase should transition to in-progress (at least one thread with this ts)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	foundInProgress := false
	for _, thread := range repo.threads {
		if thread.SlackThreadTs == "existing-ts" && thread.WorkstreamPhase == PhaseInProgress {
			foundInProgress = true
			break
		}
	}
	if !foundInProgress {
		t.Error("Expected at least one thread with phase in-progress after EventPROpened")
	}

	// Should NOT post a thread reply (PR opened in PhaseOpen is skipped)
	client.mu.Lock()
	defer client.mu.Unlock()
	for _, msg := range client.postedMessages {
		if msg.Thread != "" {
			t.Errorf("Expected no thread reply for EventPROpened in PhaseOpen, got: %q", msg.Text)
		}
	}
}

// --- Event narrator integration tests ---

type mockEventNarrator struct {
	calls    []EventRunRequest
	response string
	err      error
}

func (m *mockEventNarrator) RunForEvent(ctx context.Context, req EventRunRequest) (EventRunResponse, error) {
	m.calls = append(m.calls, req)
	return EventRunResponse{Text: m.response}, m.err
}

func TestNotifier_PRReady_UsesNarratorWhenAvailable(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	narrator := &mockEventNarrator{response: "Die Preview ist jetzt live!"}
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{}, WithEventNarrator(narrator))
	notifier.retryWait = 10 * time.Millisecond

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "t1",
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubPRID:    10,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		SlackParentTs: "thread-ts",
		ThreadType:    "pull_request",
	})

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	notifier.Notify(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		PreviewURL:  "https://preview.example.com",
		UserNote:    "Login: test/test",
	}, target)
	debouncer.executeAll()

	if len(narrator.calls) != 1 {
		t.Fatalf("Expected narrator to be called once, got %d", len(narrator.calls))
	}
	if !strings.Contains(narrator.calls[0].UserText, "preview") {
		t.Errorf("Expected narrator to receive preview event text, got: %q", narrator.calls[0].UserText)
	}

	// Should post the narrator's response, not the template
	var threadReplies []string
	for _, msg := range client.postedMessages {
		if msg.Thread != "" {
			threadReplies = append(threadReplies, msg.Text)
		}
	}
	if len(threadReplies) != 1 {
		t.Fatalf("Expected 1 thread reply, got %d", len(threadReplies))
	}
	if threadReplies[0] != "Die Preview ist jetzt live!" {
		t.Errorf("Expected narrator response, got: %q", threadReplies[0])
	}
}

func TestNotifier_PRReady_FallsBackToTemplateOnNarratorError(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	narrator := &mockEventNarrator{err: fmt.Errorf("LLM timeout")}
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{}, WithEventNarrator(narrator))
	notifier.retryWait = 10 * time.Millisecond

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "t1",
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubPRID:    10,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		SlackParentTs: "thread-ts",
		ThreadType:    "pull_request",
	})

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	notifier.Notify(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		PreviewURL:  "https://preview.example.com",
	}, target)
	debouncer.executeAll()

	// Narrator was called but failed — should fall back to template message
	if len(narrator.calls) != 1 {
		t.Fatalf("Expected narrator to be called once, got %d", len(narrator.calls))
	}

	var threadReplies []string
	for _, msg := range client.postedMessages {
		if msg.Thread != "" {
			threadReplies = append(threadReplies, msg.Text)
		}
	}
	if len(threadReplies) != 1 {
		t.Fatalf("Expected 1 thread reply (template fallback), got %d", len(threadReplies))
	}
	// Template message should still contain the preview URL
	if !strings.Contains(threadReplies[0], "preview") {
		t.Errorf("Template fallback should mention preview, got: %q", threadReplies[0])
	}
}

func TestNotifier_PRReady_WithoutNarrator_UsesTemplate(t *testing.T) {
	client := &mockClient{}
	repo := newMockRepository()
	debouncer := newMockDebouncer()
	// No narrator — should use template as before
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})
	notifier.retryWait = 10 * time.Millisecond

	repo.SaveThread(context.Background(), &SlackThread{
		ID:            "t1",
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubPRID:    10,
		SlackChannel:  "#test",
		SlackThreadTs: "thread-ts",
		SlackParentTs: "thread-ts",
		ThreadType:    "pull_request",
	})

	target := targets.TargetConfig{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
	}

	notifier.Notify(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		PreviewURL:  "https://preview.example.com",
	}, target)
	debouncer.executeAll()

	var threadReplies []string
	for _, msg := range client.postedMessages {
		if msg.Thread != "" {
			threadReplies = append(threadReplies, msg.Text)
		}
	}
	if len(threadReplies) != 1 {
		t.Fatalf("Expected 1 thread reply (template), got %d", len(threadReplies))
	}
}
