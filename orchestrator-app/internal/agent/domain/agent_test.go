package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
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
	results map[string]string
	effects SideEffects
	calls   []ToolCall
}

func (m *mockToolExecutor) Execute(_ context.Context, call ToolCall, _ targets.TargetConfig) (string, error) {
	m.calls = append(m.calls, call)
	if result, ok := m.results[call.Function.Name]; ok {
		return result, nil
	}
	return "ok", nil
}

func (m *mockToolExecutor) Effects() SideEffects {
	return m.effects
}

// mockSlackFetcher returns canned thread messages.
type mockSlackFetcher struct {
	messages []ThreadMessage
	err      error
}

func (m *mockSlackFetcher) GetThreadReplies(_ context.Context, _, _, _ string) ([]ThreadMessage, error) {
	return m.messages, m.err
}

var agentTarget = targets.TargetConfig{
	RepoOwner:     "acme",
	RepoName:      "widgets",
	GitHubPAT:     "pat-123",
	SlackBotToken: "xoxb-test",
}

func TestAgent_SimpleTextResponse(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I'll help you with that!", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	agent := NewAgent(llm, tools, nil, "test-model")

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "I'll help you with that!" {
		t.Errorf("expected agent text response, got: %s", resp.Text)
	}
	if len(tools.calls) != 0 {
		t.Errorf("expected no tool calls, got %d", len(tools.calls))
	}
}

func TestAgent_SingleToolCall(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: FunctionCall{
							Name:      "search_github_issues",
							Arguments: `{"query":"login"}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{Content: "I found some issues about login.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{
			"search_github_issues": `[{"number":1,"title":"Login bug"}]`,
		},
	}
	agent := NewAgent(llm, tools, nil, "test-model")

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Search for login issues",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "I found some issues about login." {
		t.Errorf("unexpected response: %s", resp.Text)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tools.calls))
	}
	if tools.calls[0].Function.Name != "search_github_issues" {
		t.Errorf("expected search_github_issues, got %s", tools.calls[0].Function.Name)
	}
}

func TestAgent_MultiStepToolCalls(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			// Step 1: search for duplicates
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "search_github_issues", Arguments: `{"query":"forgot password"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			// Step 2: no duplicates, create issue
			{
				ToolCalls: []ToolCall{
					{ID: "call_2", Type: "function", Function: FunctionCall{
						Name: "create_github_issue", Arguments: `{"title":"Forgot password","body":"Details"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			// Step 3: final response
			{Content: "Created issue #42!", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{
			"search_github_issues": "No issues found matching the query.",
			"create_github_issue":  "Created issue #42",
		},
	}
	agent := NewAgent(llm, tools, nil, "test-model")

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "I want a forgot password feature",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Created issue #42!" {
		t.Errorf("unexpected response: %s", resp.Text)
	}
	if len(tools.calls) != 2 {
		t.Errorf("expected 2 tool calls, got %d", len(tools.calls))
	}
}

func TestAgent_MaxIterations(t *testing.T) {
	// LLM always returns tool calls, never stops
	llm := &mockLLMClient{
		responses: make([]ChatResponse, maxIterations+1),
	}
	for i := range llm.responses {
		llm.responses[i] = ChatResponse{
			ToolCalls: []ToolCall{
				{ID: fmt.Sprintf("call_%d", i), Type: "function", Function: FunctionCall{
					Name: "search_github_issues", Arguments: `{"query":"test"}`,
				}},
			},
			FinishReason: "tool_calls",
		}
	}

	tools := &mockToolExecutor{results: map[string]string{
		"search_github_issues": "[]",
	}}
	agent := NewAgent(llm, tools, nil, "test-model")

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Loop forever",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Text, "having trouble") {
		t.Errorf("expected graceful max-iteration message, got: %s", resp.Text)
	}
}

func TestAgent_LLMError(t *testing.T) {
	llm := &mockLLMClient{
		errors: []error{fmt.Errorf("connection refused")},
	}
	tools := &mockToolExecutor{}
	agent := NewAgent(llm, tools, nil, "test-model")

	_, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestAgent_ThreadContextFetched(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Got it, context from the thread.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	fetcher := &mockSlackFetcher{
		messages: []ThreadMessage{
			{User: "U001", Text: "Original question", Ts: "100.000"},
			{User: "U002", Text: "Bot reply", Ts: "100.001", BotID: "B001"},
			{User: "U001", Text: "Follow up", Ts: "100.002"}, // this is the current message
		},
	}
	agent := NewAgent(llm, tools, fetcher, "test-model")

	_, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		ThreadTs:  "100.000",
		MessageTs: "100.002",
		UserText:  "Follow up",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check that thread messages were included (minus current message)
	// System + 2 thread messages + user message = 4 messages
	if len(llm.requests) != 1 {
		t.Fatalf("expected 1 LLM request, got %d", len(llm.requests))
	}
	msgs := llm.requests[0].Messages
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages (system + 2 thread + user), got %d", len(msgs))
	}
}

func TestAgent_LinkedIssueContext(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I see the linked issue.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	agent := NewAgent(llm, tools, nil, "test-model")

	_, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "What's the status?",
		UserName:  "alice",
		Target:    agentTarget,
		LinkedIssue: &IssueContext{
			Number: 42,
			Title:  "Login bug",
			Body:   "Users can't log in",
			State:  "open",
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msgs := llm.requests[0].Messages
	// Should have: system prompt, linked issue context (system), user message
	found := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "#42") && strings.Contains(m.Content, "Login bug") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected linked issue context in messages")
	}
}
