package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestOrchestrator_ImplementsAgentRunnerInterface is a compile-time check.
// The Orchestrator must satisfy the AgentRunner interface used by the Slack handler.
func TestOrchestrator_ImplementsAgentRunnerInterface(t *testing.T) {
	// This function body is intentionally a compile-time assertion.
	// If Orchestrator doesn't have Run(ctx, RunRequest) (RunResponse, error),
	// this won't compile.
	var _ interface {
		Run(ctx context.Context, req RunRequest) (RunResponse, error)
	} = (*Orchestrator)(nil)
}

func TestOrchestrator_SingleStep_Research(t *testing.T) {
	// Router returns researcher; researcher specialist answers with text.
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"info"}]}`, FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "There are 3 open issues.", FinishReason: "stop"},
		},
	}
	// We use a combined LLM that serves router first, then specialist.
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "What issues are open?",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "There are 3 open issues." {
		t.Errorf("unexpected response: %s", resp.Text)
	}
}

func TestOrchestrator_SingleStep_IssueCreation(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"new issue"}]}`, FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "c1", Type: "function", Function: FunctionCall{
						Name: "search_github_issues", Arguments: `{"query":"dark mode"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{
				ToolCalls: []ToolCall{
					{ID: "c2", Type: "function", Function: FunctionCall{
						Name: "create_github_issue", Arguments: `{"title":"Dark mode","body":"Add dark mode"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Created issue #42!", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{
		results: map[string]string{
			"search_github_issues": "No issues found matching the query.",
			"create_github_issue":  "Created issue #42: Dark mode\nURL: https://github.com/acme/widgets/issues/42",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 42, Title: "Dark mode"})
		}
	}

	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create an issue for dark mode",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Text, "#42") {
		t.Errorf("expected response mentioning #42, got: %s", resp.Text)
	}
	if len(resp.SideEffects.CreatedIssues) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(resp.SideEffects.CreatedIssues))
	}
}

func TestOrchestrator_RouterFallback_OnBadJSON(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "this is not json!", FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I'll research that for you.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have fallen back to researcher and gotten a response
	if resp.Text != "I'll research that for you." {
		t.Errorf("unexpected response: %s", resp.Text)
	}
}

func TestOrchestrator_MultiStep_CreateThenDelegate(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"create"},{"specialist":"delegator","params":{},"reasoning":"delegate"}]}`, FinishReason: "stop"},
		},
	}
	// Issue creator: search, create, respond
	creatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{
					Name: "create_github_issue", Arguments: `{"title":"Feature X","body":"Details"}`,
				}}},
				FinishReason: "tool_calls",
			},
			{Content: "Created issue #10!", FinishReason: "stop"},
		},
	}
	// Delegator: comment with /opencode, respond
	delegatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{{ID: "d1", Type: "function", Function: FunctionCall{
					Name: "add_github_comment", Arguments: `{"number":10,"body":"/opencode Implement Feature X"}`,
				}}},
				FinishReason: "tool_calls",
			},
			{Content: "I've asked OpenCode to implement Feature X on issue #10.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, creatorLLM, delegatorLLM}}

	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #10: Feature X\nURL: https://github.com/acme/widgets/issues/10",
			"add_github_comment":  "Comment added to issue #10.\nComment URL: https://github.com/acme/widgets/issues/10#issuecomment-999",
		},
	}
	tools.onExecute = func(call ToolCall) {
		switch call.Function.Name {
		case "create_github_issue":
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 10, Title: "Feature X"})
		case "add_github_comment":
			tools.effects.PostedComments = append(tools.effects.PostedComments, 999)
			tools.effects.DelegatedIssues = append(tools.effects.DelegatedIssues, 10)
		}
	}

	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create an issue for Feature X and delegate to OpenCode",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Final text from last specialist (delegator)
	if !strings.Contains(resp.Text, "OpenCode") {
		t.Errorf("expected delegator response, got: %s", resp.Text)
	}
	// Effects merged from both steps
	if len(resp.SideEffects.CreatedIssues) != 1 {
		t.Errorf("expected 1 created issue, got %d", len(resp.SideEffects.CreatedIssues))
	}
	if len(resp.SideEffects.DelegatedIssues) != 1 {
		t.Errorf("expected 1 delegated issue, got %d", len(resp.SideEffects.DelegatedIssues))
	}
}

func TestOrchestrator_MultiStep_PriorContextPassed(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"create"},{"specialist":"delegator","params":{},"reasoning":"delegate"}]}`, FinishReason: "stop"},
		},
	}
	// Creator: tool call then text response (no hallucination-triggering phrases)
	creatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{
					Name: "create_github_issue", Arguments: `{"title":"Test","body":"Details"}`,
				}}},
				FinishReason: "tool_calls",
			},
			{Content: "Done — issue #55 is ready.", FinishReason: "stop"},
		},
	}
	// Delegator: we'll check that it received prior context
	delegatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Delegated on #55.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, creatorLLM, delegatorLLM}}

	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #55: Test\nURL: https://github.com/acme/widgets/issues/55",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 55, Title: "Test"})
		}
	}

	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create and delegate",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The delegator's LLM requests should contain prior context from issue_creator
	if len(delegatorLLM.requests) == 0 {
		t.Fatal("expected delegator LLM to be called")
	}
	found := false
	for _, msg := range delegatorLLM.requests[0].Messages {
		if strings.Contains(msg.Content, "issue_creator") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected prior step context (issue_creator) in delegator's messages")
	}
}

func TestOrchestrator_MultiStep_CreatedIssueInjectedAsLinkedIssue(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"create"},{"specialist":"delegator","params":{},"reasoning":"delegate"}]}`, FinishReason: "stop"},
		},
	}
	// Creator: tool call then text (avoids hallucination trigger)
	creatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{
					Name: "create_github_issue", Arguments: `{"title":"Feature","body":"Details"}`,
				}}},
				FinishReason: "tool_calls",
			},
			{Content: "Done — issue #77 is ready.", FinishReason: "stop"},
		},
	}
	delegatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Delegated.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, creatorLLM, delegatorLLM}}

	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #77: Feature\nURL: https://github.com/acme/widgets/issues/77",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 77, Title: "Feature"})
		}
	}

	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create and delegate",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The delegator's LLM messages should mention issue #77
	if len(delegatorLLM.requests) == 0 {
		t.Fatal("expected delegator LLM to be called")
	}
	found := false
	for _, msg := range delegatorLLM.requests[0].Messages {
		if strings.Contains(msg.Content, "#77") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected created issue #77 in delegator's context")
	}
}

func TestOrchestrator_RouterError_ReturnsError(t *testing.T) {
	routerLLM := &mockLLMClient{
		errors: []error{fmt.Errorf("router timeout")},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM}}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "router timeout") {
		t.Errorf("expected router timeout error, got: %v", err)
	}
}

func TestOrchestrator_SpecialistError_ReturnsError(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"ok"}]}`, FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		errors: []error{fmt.Errorf("specialist timeout")},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "specialist timeout") {
		t.Errorf("expected specialist timeout error, got: %v", err)
	}
}

func TestOrchestrator_UnknownSpecialist_FallsBackToResearcher(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"unknown_agent","params":{},"reasoning":"?"}]}`, FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I'll help with that.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "hello",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "I'll help with that." {
		t.Errorf("unexpected response: %s", resp.Text)
	}
}

func TestOrchestrator_ThreadContextFetched(t *testing.T) {
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"ok"}]}`, FinishReason: "stop"},
		},
	}
	specialistLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Got it from thread.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, specialistLLM}}

	tools := &mockToolExecutor{}
	fetcher := &mockSlackFetcher{
		messages: []ThreadMessage{
			{User: "U001", Text: "Original question", Ts: "100.000"},
			{User: "U001", Text: "Follow up", Ts: "100.002"},
		},
	}
	orch := NewOrchestrator(combinedLLM, tools, fetcher, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
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

	// The router's LLM should have received thread context
	if len(routerLLM.requests) == 0 {
		t.Fatal("expected router LLM to be called")
	}
	routerFound := false
	for _, msg := range routerLLM.requests[0].Messages {
		if strings.Contains(msg.Content, "Conversation history") {
			routerFound = true
			break
		}
	}
	if !routerFound {
		t.Error("expected thread context in router's LLM request")
	}

	// The specialist's LLM should have received thread context
	if len(specialistLLM.requests) == 0 {
		t.Fatal("expected specialist LLM to be called")
	}
	// system + thread msg + user = at least 3 messages
	msgCount := len(specialistLLM.requests[0].Messages)
	if msgCount < 3 {
		t.Errorf("expected at least 3 messages (system + thread + user), got %d", msgCount)
	}
}

func TestOrchestrator_ThreadContextFetchedOnce_NotPerSpecialist(t *testing.T) {
	// Multi-step chain with thread context — fetcher should be called only once.
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"create"},{"specialist":"delegator","params":{},"reasoning":"delegate"}]}`, FinishReason: "stop"},
		},
	}
	creatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Created issue #10.", FinishReason: "stop"},
		},
	}
	delegatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Delegated.", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, creatorLLM, delegatorLLM}}

	tools := &mockToolExecutor{}
	fetcher := &mockSlackFetcher{
		messages: []ThreadMessage{
			{User: "U001", Text: "Original question", Ts: "100.000"},
			{User: "U002", Text: "Follow up", Ts: "100.001"},
		},
	}
	orch := NewOrchestrator(combinedLLM, tools, fetcher, "test-model", OrchestratorConfig{})

	_, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		ThreadTs:  "100.000",
		MessageTs: "100.001",
		UserText:  "Create and delegate",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetcher.callCount != 1 {
		t.Errorf("expected slack fetcher called once, got %d", fetcher.callCount)
	}
}

func TestOrchestrator_MaxSteps_TruncatesExcessiveRouting(t *testing.T) {
	// Router returns 8 steps — orchestrator should only execute maxOrchestratorSteps (5).
	steps := `{"steps":[` +
		`{"specialist":"researcher","params":{},"reasoning":"s1"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s2"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s3"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s4"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s5"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s6"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s7"},` +
		`{"specialist":"researcher","params":{},"reasoning":"s8"}` +
		`]}`
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: steps, FinishReason: "stop"},
		},
	}

	// Each specialist call produces a simple text response.
	var specialistLLMs []*mockLLMClient
	for i := 0; i < 8; i++ {
		specialistLLMs = append(specialistLLMs, &mockLLMClient{
			responses: []ChatResponse{
				{Content: fmt.Sprintf("response %d", i+1), FinishReason: "stop"},
			},
		})
	}

	allClients := append([]*mockLLMClient{routerLLM}, specialistLLMs...)
	combinedLLM := &sequentialMockLLM{clients: allClients}

	tools := &mockToolExecutor{}
	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "do many things",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Last text should be from the 5th specialist, not the 8th
	if resp.Text != "response 5" {
		t.Errorf("expected response from 5th step, got: %s", resp.Text)
	}
}

func TestOrchestrator_Reroute(t *testing.T) {
	// Router sends to researcher; researcher signals reroute to issue_creator;
	// orchestrator should invoke issue_creator and return its result.
	routerLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"info"}]}`, FinishReason: "stop"},
		},
	}
	// Researcher responds with a reroute signal
	researcherLLM := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "[REROUTE:issue_creator]", FinishReason: "stop"},
		},
	}
	// Issue creator handles the request
	issueCreatorLLM := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{
					Name: "create_github_issue", Arguments: `{"title":"New issue","body":"Details"}`,
				}}},
				FinishReason: "tool_calls",
			},
			{Content: "Created issue #50!", FinishReason: "stop"},
		},
	}
	combinedLLM := &sequentialMockLLM{clients: []*mockLLMClient{routerLLM, researcherLLM, issueCreatorLLM}}

	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #50: New issue\nURL: https://github.com/acme/widgets/issues/50",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 50, Title: "New issue"})
		}
	}

	orch := NewOrchestrator(combinedLLM, tools, nil, "test-model", OrchestratorConfig{})

	resp, err := orch.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "let's start fresh",
		UserName:  "alice",
		Target:    agentTarget,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(resp.Text, "#50") {
		t.Errorf("expected issue_creator response, got: %s", resp.Text)
	}
	if len(resp.SideEffects.CreatedIssues) != 1 {
		t.Errorf("expected 1 created issue from rerouted specialist, got %d", len(resp.SideEffects.CreatedIssues))
	}
}

// --- Test helpers ---

// sequentialMockLLM serves responses from multiple mock LLM clients in order.
// First client handles all its calls, then the next client handles subsequent calls, etc.
type sequentialMockLLM struct {
	clients   []*mockLLMClient
	clientIdx int
}

func (s *sequentialMockLLM) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if s.clientIdx >= len(s.clients) {
		return ChatResponse{Content: "fallback", FinishReason: "stop"}, nil
	}

	client := s.clients[s.clientIdx]
	resp, err := client.ChatCompletion(ctx, req)

	// If this client has exhausted all its responses, advance to the next client
	if client.callIdx >= len(client.responses) && (client.callIdx >= len(client.errors) || client.errors == nil) {
		s.clientIdx++
	}

	return resp, err
}
