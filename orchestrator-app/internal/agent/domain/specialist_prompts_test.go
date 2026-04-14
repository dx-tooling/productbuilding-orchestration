package domain

import (
	"bytes"
	"strings"
	"testing"
)

func TestDelegatorPrompt_ContainsFeedbackRelayGuidance(t *testing.T) {
	var buf bytes.Buffer
	if err := delegatorPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	if !strings.Contains(strings.ToLower(prompt), "feedback relay") || !strings.Contains(prompt, "review") {
		t.Error("expected delegator prompt to contain feedback relay guidance for review phase")
	}
}

func TestEventNarratorPrompt_ContainsNoToolsInstruction(t *testing.T) {
	var buf bytes.Buffer
	if err := eventNarratorPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	if !strings.Contains(prompt, "Do NOT call any tools") {
		t.Error("event narrator prompt must instruct LLM not to call tools")
	}
	if !strings.Contains(prompt, "natural") || !strings.Contains(prompt, "friendly") {
		t.Error("event narrator prompt must instruct conversational, friendly tone")
	}
}

func TestEventNarratorTools_ReturnsEmpty(t *testing.T) {
	tools := EventNarratorTools()
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for event_narrator, got %d", len(tools))
	}
}

func TestCloserPrompt_ForbidsRefetchAfterClose(t *testing.T) {
	var buf bytes.Buffer
	if err := closerPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	if !strings.Contains(prompt, "Do not") || !strings.Contains(prompt, "get_github_issue") {
		t.Error("closer prompt must instruct LLM not to re-fetch state after closing")
	}
	if !strings.Contains(prompt, "trust the tool result") {
		t.Error("closer prompt must instruct LLM to trust the tool result")
	}
}

func TestIssueCreatorPrompt_ContainsIntakeGuidance(t *testing.T) {
	var buf bytes.Buffer
	if err := issueCreatorPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	if !strings.Contains(prompt, "intake") || !strings.Contains(prompt, "clarif") {
		t.Error("expected issue_creator prompt to contain intake clarification guidance")
	}
}
