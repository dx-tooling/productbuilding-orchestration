package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func newTestSpecialist(llm LLMClient, tools ToolExecutor, opts ...func(*Specialist)) *Specialist {
	s := &Specialist{
		config: SpecialistConfig{
			Name:          "test_specialist",
			SystemPrompt:  "You are a test specialist for {{.RepoOwner}}/{{.RepoName}}.",
			ToolDefs:      IssueCreatorTools(),
			MaxIterations: 5,
		},
		llm:         llm,
		tools:       tools,
		tokenBudget: DefaultTokenBudget(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestSpecialist_SimpleTextResponse(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "Here's your answer!", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Here's your answer!" {
		t.Errorf("expected text response, got: %s", result.Text)
	}
}

func TestSpecialist_SingleToolCall(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "search_github_issues", Arguments: `{"query":"dark mode"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Found some issues about dark mode.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{
		results: map[string]string{
			"search_github_issues": `[{"number":1,"title":"Dark mode request"}]`,
		},
	}
	s := newTestSpecialist(llm, tools)

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Search for dark mode",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Found some issues about dark mode." {
		t.Errorf("unexpected response: %s", result.Text)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(tools.calls))
	}
	if tools.calls[0].Function.Name != "search_github_issues" {
		t.Errorf("expected search_github_issues, got %s", tools.calls[0].Function.Name)
	}
}

func TestSpecialist_MaxIterations(t *testing.T) {
	maxIter := 3
	llm := &mockLLMClient{
		responses: make([]ChatResponse, maxIter+1),
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

	tools := &mockToolExecutor{results: map[string]string{"search_github_issues": "[]"}}
	s := newTestSpecialist(llm, tools)
	s.config.MaxIterations = maxIter

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Loop forever",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(result.Text, "having trouble") {
		t.Error("should not return generic 'having trouble' message when tool results exist")
	}
	if !strings.Contains(result.Text, "I wasn't able to fully answer") {
		t.Errorf("expected partial-findings disclaimer, got: %s", result.Text)
	}
}

func TestSpecialist_MaxIterations_WithPartialAssistantText(t *testing.T) {
	maxIter := 2
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				Content: "Let me search for that file...",
				ToolCalls: []ToolCall{
					{ID: "call_0", Type: "function", Function: FunctionCall{
						Name: "search_github_issues", Arguments: `{"query":"startpage"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{
				Content: "I found the template in src/startpage.html, let me read it...",
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "search_github_issues", Arguments: `{"query":"startpage template"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
		},
	}

	tools := &mockToolExecutor{results: map[string]string{"search_github_issues": "found: src/startpage.html"}}
	s := newTestSpecialist(llm, tools)
	s.config.MaxIterations = maxIter

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Show me the startpage template",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result.Text, "I wasn't able to fully answer") {
		t.Errorf("expected partial-findings disclaimer, got: %s", result.Text)
	}
	if !strings.Contains(result.Text, "I found the template in src/startpage.html") {
		t.Errorf("expected last assistant text in partial findings, got: %s", result.Text)
	}
}

func TestSpecialist_LLMError_Propagates(t *testing.T) {
	llm := &mockLLMClient{
		errors: []error{fmt.Errorf("timeout")},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)

	_, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("expected timeout error, got: %v", err)
	}
}

func TestSpecialist_UsesConfiguredPrompt(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)
	s.config.SystemPrompt = "You are the CUSTOM specialist for {{.RepoOwner}}/{{.RepoName}}."

	_, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) == 0 {
		t.Fatal("expected at least 1 LLM request")
	}
	systemMsg := llm.requests[0].Messages[0]
	if !strings.Contains(systemMsg.Content, "CUSTOM specialist") {
		t.Errorf("expected custom prompt, got: %s", systemMsg.Content)
	}
	if !strings.Contains(systemMsg.Content, "acme/widgets") {
		t.Errorf("expected repo name in prompt, got: %s", systemMsg.Content)
	}
}

func TestSpecialist_UsesConfiguredToolDefs(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)
	s.config.ToolDefs = CloserTools() // only 3 tools

	_, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Close issue #7",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) == 0 {
		t.Fatal("expected at least 1 LLM request")
	}
	if len(llm.requests[0].Tools) != 3 {
		t.Errorf("expected 3 tools (closer set), got %d", len(llm.requests[0].Tools))
	}
}

func TestSpecialist_HallucinationGuard(t *testing.T) {
	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #42: Dark mode\nURL: https://github.com/acme/widgets/issues/42",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 42, Title: "Dark mode"})
		}
	}

	llm := &mockLLMClient{
		responses: []ChatResponse{
			// Hallucinate: claim issue created without tool call
			{Content: "I've created issue #42 for dark mode.", FinishReason: "stop"},
			// After correction: actually call the tool
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "create_github_issue", Arguments: `{"title":"Dark mode","body":"Add dark mode support"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			// Truthful response
			{Content: "I've created issue #42 for dark mode.", FinishReason: "stop"},
		},
	}

	s := newTestSpecialist(llm, tools)

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create an issue for dark mode",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools.calls) != 1 {
		t.Fatalf("expected 1 tool call after correction, got %d", len(tools.calls))
	}
	if result.SideEffects.CreatedIssues[0].Number != 42 {
		t.Errorf("expected created issue #42, got %v", result.SideEffects.CreatedIssues)
	}
}

func TestSpecialist_PriorContextInjected(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I see the prior context.", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)

	prior := &PriorStepContext{
		StepName:   "issue_creator",
		ResultText: "Created issue #42: Dark mode",
		Effects:    SideEffects{CreatedIssues: []CreatedIssue{{Number: 42, Title: "Dark mode"}}},
	}

	_, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Now delegate it",
		UserName:  "alice",
		Target:    agentTarget,
	}, prior)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) == 0 {
		t.Fatal("expected at least 1 LLM request")
	}
	found := false
	for _, msg := range llm.requests[0].Messages {
		if strings.Contains(msg.Content, "issue_creator") && strings.Contains(msg.Content, "#42") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected prior step context in messages")
	}
}

func TestSpecialist_ListConversations_Intercepted(t *testing.T) {
	lister := &mockConversationLister{
		conversations: []Conversation{
			{
				ChannelID:    "C123",
				ThreadTs:     "1111111111.111111",
				Summary:      "Dark mode discussion",
				UserName:     "alice",
				LastActiveAt: time.Now(),
			},
		},
	}

	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_list", Type: "function", Function: FunctionCall{
						Name: "list_conversations", Arguments: `{"days":7}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Here are your conversations!", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools, func(s *Specialist) {
		s.conversationLister = lister
		s.workspace = "test-workspace"
		s.config.ToolDefs = ResearcherTools()
	})

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "What have we discussed?",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Text != "Here are your conversations!" {
		t.Errorf("unexpected response: %s", result.Text)
	}
	// list_conversations should NOT go through ToolExecutor
	if len(tools.calls) != 0 {
		t.Errorf("expected 0 tool executor calls, got %d", len(tools.calls))
	}
	if lister.calledWith.channelID != "C123" {
		t.Errorf("expected channelID C123, got %s", lister.calledWith.channelID)
	}
}

func TestSpecialist_TemplateRenderedFromConfigTemplate(t *testing.T) {
	// Verify that when SpecialistConfig holds a *template.Template,
	// renderPrompt produces the correctly rendered output.
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "ok", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)
	s.config.PromptTemplate = issueCreatorPromptTmpl
	s.config.SystemPrompt = "" // should be ignored when PromptTemplate is set

	_, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Hello",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) == 0 {
		t.Fatal("expected at least 1 LLM request")
	}
	sysMsg := llm.requests[0].Messages[0]
	if !strings.Contains(sysMsg.Content, "Issue Creator") {
		t.Errorf("expected issue creator prompt, got: %s", sysMsg.Content)
	}
	if !strings.Contains(sysMsg.Content, "acme/widgets") {
		t.Errorf("expected rendered repo context, got: %s", sysMsg.Content)
	}
}

func TestSpecialist_SideEffectsReturned(t *testing.T) {
	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #99: Test\nURL: https://github.com/acme/widgets/issues/99",
		},
	}
	tools.onExecute = func(call ToolCall) {
		if call.Function.Name == "create_github_issue" {
			tools.effects.CreatedIssues = append(tools.effects.CreatedIssues, CreatedIssue{Number: 99, Title: "Test"})
		}
	}

	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "create_github_issue", Arguments: `{"title":"Test","body":"Body"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Created issue #99!", FinishReason: "stop"},
		},
	}
	s := newTestSpecialist(llm, tools)

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create a test issue",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.SideEffects.CreatedIssues) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(result.SideEffects.CreatedIssues))
	}
	if result.SideEffects.CreatedIssues[0].Number != 99 {
		t.Errorf("expected issue #99, got #%d", result.SideEffects.CreatedIssues[0].Number)
	}
}

func TestExtractReroute(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"[REROUTE:issue_creator]", "issue_creator"},
		{"Some text [REROUTE:delegator] more text", "delegator"},
		{"No reroute here", ""},
		{"[REROUTE:closer]", "closer"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractReroute(tt.input)
		if got != tt.want {
			t.Errorf("extractReroute(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStripReroute(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"[REROUTE:issue_creator]", ""},
		{"Some text [REROUTE:delegator] more text", "Some text  more text"},
		{"No reroute here", "No reroute here"},
		{"Create this please [REROUTE:issue_creator]", "Create this please"},
	}
	for _, tt := range tests {
		got := stripReroute(tt.input)
		if got != tt.want {
			t.Errorf("stripReroute(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSpecialist_RerouteSignalPopulated(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "[REROUTE:issue_creator]", FinishReason: "stop"},
		},
	}
	tools := &mockToolExecutor{}
	s := newTestSpecialist(llm, tools)

	result, err := s.Run(context.Background(), RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create an issue please",
		UserName:  "alice",
		Target:    agentTarget,
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Reroute != "issue_creator" {
		t.Errorf("expected Reroute=issue_creator, got %q", result.Reroute)
	}
	if result.Text != "" {
		t.Errorf("expected empty text after stripping reroute, got %q", result.Text)
	}
}

func TestSpecialist_PassesOnIssueCreatedToTools(t *testing.T) {
	// LLM makes a tool call that creates an issue, then returns text
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{
						Name: "create_github_issue", Arguments: `{"title":"New bug","body":"Details"}`,
					}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Created the issue.", FinishReason: "stop"},
		},
	}

	callbackFired := false
	tools := &mockToolExecutor{
		results: map[string]string{
			"create_github_issue": "Created issue #42",
		},
	}
	// Set onExecute after tools is declared to avoid reference issue
	tools.onExecute = func(tc ToolCall) {
		if tc.Function.Name == "create_github_issue" && tools.onIssueCreated != nil {
			tools.onIssueCreated("acme", "widgets", 42, "New bug")
			callbackFired = true
		}
	}

	s := newTestSpecialist(llm, tools)

	req := RunRequest{
		ChannelID: "C123",
		MessageTs: "123.456",
		UserText:  "Create an issue",
		UserName:  "alice",
		Target:    agentTarget,
		OnIssueCreated: func(owner, repo string, number int, title string) {
			// This should be passed through to the tool executor
		},
	}

	_, err := s.Run(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !callbackFired {
		t.Error("Expected OnIssueCreated callback to be passed to tools and fired")
	}
}
