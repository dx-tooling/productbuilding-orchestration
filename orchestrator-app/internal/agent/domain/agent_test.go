package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

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
	results   map[string]string
	effects   SideEffects
	calls     []ToolCall
	onExecute func(ToolCall) // optional callback to mutate effects on execution
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
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 42, Title: "Forgot password"})
		}
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

func TestAgent_ListConversations_Intercepted(t *testing.T) {
	lister := &mockConversationLister{
		conversations: []Conversation{
			{
				ChannelID:    "C123",
				ThreadTs:     "1111111111.111111",
				Summary:      "Implement sign in feature",
				UserName:     "alice",
				LastActiveAt: time.Now(),
			},
			{
				ChannelID:    "C123",
				ThreadTs:     "2222222222.222222",
				Summary:      "Fix sign up bug",
				UserName:     "bob",
				LastActiveAt: time.Now().Add(-time.Hour),
			},
		},
	}

	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{
						ID:   "call_list",
						Type: "function",
						Function: FunctionCall{
							Name:      "list_conversations",
							Arguments: `{"days":7}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Here are your recent conversations!", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	agent := NewAgent(llm, tools, nil, "test-model",
		WithConversationLister(lister, "test-workspace"),
	)

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "What have we talked about?",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "Here are your recent conversations!" {
		t.Errorf("unexpected response: %s", resp.Text)
	}
	// list_conversations should NOT go through the ToolExecutor
	if len(tools.calls) != 0 {
		t.Errorf("expected 0 tool executor calls, got %d", len(tools.calls))
	}
	// Verify the lister was called with correct channel and days
	if lister.calledWith.channelID != "C123" {
		t.Errorf("expected channelID C123, got %s", lister.calledWith.channelID)
	}
	if lister.calledWith.days != 7 {
		t.Errorf("expected days 7, got %d", lister.calledWith.days)
	}
	// Verify the tool result contains deep links
	if len(llm.requests) < 2 {
		t.Fatalf("expected at least 2 LLM requests, got %d", len(llm.requests))
	}
	toolResultMsg := llm.requests[1].Messages
	found := false
	for _, m := range toolResultMsg {
		if m.Role == "tool" && strings.Contains(m.Content, "test-workspace.slack.com") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected tool result to contain Slack deep links")
	}
}

func TestAgent_ListConversations_WithoutLister_FallsThrough(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{
						ID:   "call_list",
						Type: "function",
						Function: FunctionCall{
							Name:      "list_conversations",
							Arguments: `{}`,
						},
					},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Unknown tool.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	// No WithConversationLister — should fall through to ToolExecutor
	agent := NewAgent(llm, tools, nil, "test-model")

	_, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "What have we talked about?",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have been dispatched to ToolExecutor (which won't know the tool)
	if len(tools.calls) != 1 {
		t.Errorf("expected 1 tool executor call, got %d", len(tools.calls))
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

func TestAgent_HallucinationGuard_SelfCorrects(t *testing.T) {
	// Iteration 1: LLM hallucinates ("I've asked OpenCode...") with no tool call
	// Iteration 2: after correction, LLM makes the actual tool call
	// Iteration 3: LLM responds truthfully with effects now populated
	tools := &mockToolExecutor{
		results: map[string]string{
			"add_github_comment": "Comment posted (id: 777)",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "add_github_comment" {
			tools.effects.PostedComments = append(tools.effects.PostedComments, 777)
			tools.effects.DelegatedIssues = append(tools.effects.DelegatedIssues, 42)
		}
	}

	llm := &mockLLMClient{
		responses: []ChatResponse{
			// Iter 1: hallucination — claims delegation without tool call
			{Content: "I've asked OpenCode to create a plan on issue #42.", FinishReason: "stop"},
			// Iter 2: after correction, makes the tool call
			{
				ToolCalls: []ToolCall{
					{ID: "call_fix", Type: "function", Function: FunctionCall{
						Name:      "add_github_comment",
						Arguments: `{"issue_number":42,"body":"/opencode plan"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			// Iter 3: truthful response
			{Content: "Done — I've asked OpenCode to create a plan on issue #42.", FinishReason: "stop"},
		},
	}

	agent := NewAgent(llm, tools, nil, "test-model")
	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Please delegate issue 42 to OpenCode",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The hallucinated first response should NOT be returned
	if !strings.Contains(resp.Text, "Done") {
		t.Errorf("expected truthful response, got: %s", resp.Text)
	}
	// Tool should have been called
	if len(tools.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tools.calls))
	}
	if tools.calls[0].Function.Name != "add_github_comment" {
		t.Errorf("expected add_github_comment, got %s", tools.calls[0].Function.Name)
	}
	// Side effects should reflect actual delegation
	if len(resp.SideEffects.DelegatedIssues) != 1 || resp.SideEffects.DelegatedIssues[0] != 42 {
		t.Errorf("expected DelegatedIssues=[42], got %v", resp.SideEffects.DelegatedIssues)
	}
}

func TestAgent_HallucinationGuard_ExhaustsIterations(t *testing.T) {
	// LLM hallucinates every iteration — should eventually hit maxIterations
	responses := make([]ChatResponse, maxIterations+1)
	for i := range responses {
		responses[i] = ChatResponse{
			Content:      "I've asked OpenCode to handle this.",
			FinishReason: "stop",
		}
	}

	llm := &mockLLMClient{responses: responses}
	tools := &mockToolExecutor{}
	agent := NewAgent(llm, tools, nil, "test-model")

	resp, err := agent.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Delegate this",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should hit the max iterations fallback
	if !strings.Contains(resp.Text, "having trouble") {
		t.Errorf("expected max-iteration message, got: %s", resp.Text)
	}
}

func TestAgent_HallucinationGuard_NoBenignFalsePositive(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I can help you with that! Let me search for existing issues.", FinishReason: "stop"},
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
	// Should pass through immediately — no hallucination detected
	if resp.Text != "I can help you with that! Let me search for existing issues." {
		t.Errorf("expected benign response to pass through, got: %s", resp.Text)
	}
	// Only one LLM call should have been made
	if llm.callIdx != 1 {
		t.Errorf("expected 1 LLM call (no retry), got %d", llm.callIdx)
	}
}
