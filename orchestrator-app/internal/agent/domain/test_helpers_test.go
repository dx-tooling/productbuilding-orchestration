package domain

import (
	"context"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// mockLLMClient allows scripting a sequence of LLM responses.
type mockLLMClient struct {
	responses []ChatResponse
	errors    []error
	callIdx   int
	requests  []ChatRequest
}

func (m *mockLLMClient) ChatCompletion(_ context.Context, req ChatRequest) (ChatResponse, error) {
	idx := m.callIdx
	m.callIdx++
	m.requests = append(m.requests, req)
	if idx < len(m.errors) && m.errors[idx] != nil {
		return ChatResponse{}, m.errors[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return ChatResponse{Content: "fallback", FinishReason: "stop"}, nil
}

// mockToolExecutor records calls and returns canned results.
type mockToolExecutor struct {
	results        map[string]string
	effects        SideEffects
	calls          []ToolCall
	onExecute      func(ToolCall) // optional callback to mutate effects on execution
	onIssueCreated func(owner, repo string, number int, title string)
}

func (m *mockToolExecutor) Execute(_ context.Context, call ToolCall, _ targets.TargetConfig) (string, error) {
	m.calls = append(m.calls, call)
	if m.onExecute != nil {
		m.onExecute(call)
	}
	if result, ok := m.results[call.Function.Name]; ok {
		return result, nil
	}
	return "ok", nil
}

func (m *mockToolExecutor) Effects() SideEffects {
	return m.effects
}

func (m *mockToolExecutor) ResetEffects() {
	m.effects = SideEffects{}
}

func (m *mockToolExecutor) SetOnIssueCreated(fn func(owner, repo string, number int, title string)) {
	m.onIssueCreated = fn
}

// mockSlackFetcher returns canned thread messages and counts calls.
type mockSlackFetcher struct {
	messages  []ThreadMessage
	err       error
	callCount int
}

func (m *mockSlackFetcher) GetThreadReplies(_ context.Context, _, _, _ string) ([]ThreadMessage, error) {
	m.callCount++
	return m.messages, m.err
}

// mockConversationLister returns canned conversation results.
type mockConversationLister struct {
	conversations []Conversation
	err           error
	calledWith    struct {
		channelID string
		days      int
	}
}

func (m *mockConversationLister) ListRecentConversations(_ context.Context, channelID string, days int) ([]Conversation, error) {
	m.calledWith.channelID = channelID
	m.calledWith.days = days
	return m.conversations, m.err
}

var agentTarget = targets.TargetConfig{
	RepoOwner:     "acme",
	RepoName:      "widgets",
	GitHubPAT:     "pat-123",
	SlackBotToken: "xoxb-test",
}

// Ensure mockConversationLister's calledWith.days has a usable zero value for tests that check it.
var _ = time.Now // keep time import for any test that needs it
