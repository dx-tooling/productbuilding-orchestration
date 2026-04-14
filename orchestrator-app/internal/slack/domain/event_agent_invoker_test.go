package domain

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// mockEventAgentRunner records Run calls and returns configurable responses.
type mockEventAgentRunner struct {
	mu       sync.Mutex
	calls    []EventRunRequest
	response EventRunResponse
	err      error
}

func (m *mockEventAgentRunner) RunForEvent(ctx context.Context, req EventRunRequest) (EventRunResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	return m.response, m.err
}

// mockEventThreadFinder returns a configurable thread on lookup.
type mockEventThreadFinder struct {
	mu      sync.Mutex
	calls   int
	results []*SlackThread // one per call; cycles if needed
}

func (m *mockEventThreadFinder) FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*SlackThread, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := m.calls
	m.calls++
	if idx < len(m.results) {
		return m.results[idx], nil
	}
	if len(m.results) > 0 {
		return m.results[len(m.results)-1], nil
	}
	return nil, nil
}

// mockEventPoster records PostToThread calls.
type mockEventPoster struct {
	mu    sync.Mutex
	posts []mockEventPost
}

type mockEventPost struct {
	Channel  string
	ThreadTs string
	Text     string
}

func (m *mockEventPoster) PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.posts = append(m.posts, mockEventPost{
		Channel:  channel,
		ThreadTs: threadTs,
		Text:     msg.Text,
	})
	return nil
}

var invokerTestTarget = targets.TargetConfig{
	RepoOwner:     "acme",
	RepoName:      "widgets",
	GitHubPAT:     "pat-123",
	SlackChannel:  "#productbuilding-widgets",
	SlackBotToken: "xoxb-test",
}

func TestEventAgentInvoker_HappyPath(t *testing.T) {
	thread := &SlackThread{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubIssueID: 10,
		SlackChannel:  "#productbuilding-widgets",
		SlackThreadTs: "1234.5678",
	}
	finder := &mockEventThreadFinder{results: []*SlackThread{thread}}
	runner := &mockEventAgentRunner{response: EventRunResponse{Text: "PR #10 has been merged — your feature is live now."}}
	poster := &mockEventPoster{}

	invoker := NewEventAgentInvoker(runner, finder, poster, 10*time.Millisecond)
	invoker.InvokeForEvent(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Title:       "Add dark mode",
	}, invokerTestTarget)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 agent call, got %d", len(runner.calls))
	}
	if !strings.HasPrefix(runner.calls[0].UserText, "[system event]") {
		t.Errorf("expected UserText to start with [system event], got %q", runner.calls[0].UserText)
	}

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(poster.posts))
	}
	if poster.posts[0].ThreadTs != "1234.5678" {
		t.Errorf("expected post to thread 1234.5678, got %q", poster.posts[0].ThreadTs)
	}
	if !strings.Contains(poster.posts[0].Text, "merged") {
		t.Errorf("expected agent response in post, got %q", poster.posts[0].Text)
	}
}

func TestEventAgentInvoker_ThreadNotFoundInitially_RetrySucceeds(t *testing.T) {
	thread := &SlackThread{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubIssueID: 10,
		SlackChannel:  "#productbuilding-widgets",
		SlackThreadTs: "1234.5678",
	}
	// First call returns nil, second returns the thread
	finder := &mockEventThreadFinder{results: []*SlackThread{nil, thread}}
	runner := &mockEventAgentRunner{response: EventRunResponse{Text: "CI failed on build step."}}
	poster := &mockEventPoster{}

	invoker := NewEventAgentInvoker(runner, finder, poster, 10*time.Millisecond)
	invoker.InvokeForEvent(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventCIFailed,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}, invokerTestTarget)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 1 {
		t.Fatalf("expected 1 agent call after retry, got %d", len(runner.calls))
	}

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 1 {
		t.Fatalf("expected 1 post after retry, got %d", len(poster.posts))
	}
}

func TestEventAgentInvoker_ThreadNeverFound(t *testing.T) {
	finder := &mockEventThreadFinder{results: []*SlackThread{nil, nil}}
	runner := &mockEventAgentRunner{response: EventRunResponse{Text: "should not be called"}}
	poster := &mockEventPoster{}

	invoker := NewEventAgentInvoker(runner, finder, poster, 10*time.Millisecond)
	invoker.InvokeForEvent(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}, invokerTestTarget)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.calls) != 0 {
		t.Errorf("expected 0 agent calls when thread never found, got %d", len(runner.calls))
	}

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 0 {
		t.Errorf("expected 0 posts when thread never found, got %d", len(poster.posts))
	}
}

func TestEventAgentInvoker_AgentError_NoPost(t *testing.T) {
	thread := &SlackThread{
		RepoOwner:     "acme",
		RepoName:      "widgets",
		GithubIssueID: 10,
		SlackChannel:  "#productbuilding-widgets",
		SlackThreadTs: "1234.5678",
	}
	finder := &mockEventThreadFinder{results: []*SlackThread{thread}}
	runner := &mockEventAgentRunner{err: fmt.Errorf("LLM timeout")}
	poster := &mockEventPoster{}

	invoker := NewEventAgentInvoker(runner, finder, poster, 10*time.Millisecond)
	invoker.InvokeForEvent(context.Background(), slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
	}, invokerTestTarget)

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 0 {
		t.Errorf("expected 0 posts when agent errors, got %d", len(poster.posts))
	}
}

func TestFormatSystemEvent_PRMerged(t *testing.T) {
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		IssueNumber: 17,
	}
	text := formatSystemEvent(event)
	if !strings.HasPrefix(text, "[system event]") {
		t.Errorf("expected [system event] prefix, got %q", text)
	}
	if !strings.Contains(text, "#17") {
		t.Errorf("expected PR number in text, got %q", text)
	}
	if !strings.Contains(text, "merged") {
		t.Errorf("expected 'merged' in text, got %q", text)
	}
}

func TestFormatSystemEvent_CIFailed(t *testing.T) {
	event := slackfacade.NotificationEvent{
		Type:           slackfacade.EventCIFailed,
		CheckRunName:   "build",
		FailureSummary: "exit code 1",
		WorkflowURL:    "https://github.com/acme/widgets/runs/100",
	}
	text := formatSystemEvent(event)
	if !strings.Contains(text, "build") {
		t.Errorf("expected check name in text, got %q", text)
	}
}

func TestFormatSystemEvent_PRReady(t *testing.T) {
	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview.example.com",
	}
	text := formatSystemEvent(event)
	if !strings.Contains(text, "preview.example.com") {
		t.Errorf("expected preview URL in text, got %q", text)
	}
}

func TestFormatSystemEvent_CommentAdded(t *testing.T) {
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		Author:      "bob",
		IssueNumber: 5,
		Body:        "Looks good, let's ship it",
	}
	text := formatSystemEvent(event)
	if !strings.Contains(text, "@bob") {
		t.Errorf("expected @bob in text, got %q", text)
	}
	if !strings.Contains(text, "Looks good") {
		t.Errorf("expected comment body in text, got %q", text)
	}
}
