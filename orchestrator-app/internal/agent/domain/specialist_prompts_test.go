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
