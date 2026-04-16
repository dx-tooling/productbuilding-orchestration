package domain

import (
	"bytes"
	"strings"
	"testing"
	"text/template"
)

func TestPromptData_HasLanguageField(t *testing.T) {
	pd := PromptData{RepoOwner: "acme", RepoName: "widgets", Language: "de"}
	if pd.Language != "de" {
		t.Errorf("got %q, want %q", pd.Language, "de")
	}
}

func TestAllSpecialistPrompts_ContainLanguageInstruction(t *testing.T) {
	templates := map[string]*template.Template{
		"issue_creator":  issueCreatorPromptTmpl,
		"delegator":      delegatorPromptTmpl,
		"commenter":      commenterPromptTmpl,
		"researcher":     researcherPromptTmpl,
		"event_narrator": eventNarratorPromptTmpl,
		"closer":         closerPromptTmpl,
	}
	for name, tmpl := range templates {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			err := tmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets", Language: "de"})
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			prompt := buf.String()
			if !strings.Contains(prompt, "MUST respond in") {
				t.Errorf("%s: missing language instruction", name)
			}
			if !strings.Contains(prompt, "respond in de") {
				t.Errorf("%s: language code 'de' not rendered in instruction", name)
			}
		})
	}
}

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

func TestEventNarratorPrompt_NeverPromisesActions(t *testing.T) {
	var buf bytes.Buffer
	if err := eventNarratorPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	// The event_narrator has no tools. It must never promise to take action.
	// "indicate you are looking into it" caused the LLM to promise follow-up it couldn't deliver.
	if strings.Contains(prompt, "looking into it") {
		t.Error("event narrator must not say 'looking into it' — it has no tools and cannot take action")
	}
	if strings.Contains(prompt, "offer to investigate") {
		t.Error("event narrator must not 'offer to investigate' — it has no tools; it should tell the user to ask for investigation")
	}

	// Must explicitly instruct: never promise actions, ask the user instead
	if !strings.Contains(prompt, "NEVER promise") {
		t.Error("event narrator prompt must explicitly forbid promising actions")
	}
	if !strings.Contains(prompt, "ask the user") {
		t.Error("event narrator prompt must instruct the LLM to ask the user what to do next")
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

func TestDelegatorPrompt_ForbidsBranchAndPRInstructions(t *testing.T) {
	var buf bytes.Buffer
	if err := delegatorPromptTmpl.Execute(&buf, PromptData{RepoOwner: "acme", RepoName: "widgets"}); err != nil {
		t.Fatalf("execute template: %v", err)
	}
	prompt := buf.String()

	// Rule 3 currently says "OpenCode will create branches and PRs" which is wrong —
	// the CI framework creates branches and PRs, and OpenCode must NOT do this itself.
	// The prompt must contain an explicit, general prohibition (not just for plans).
	if strings.Contains(prompt, "OpenCode will create branches") {
		t.Error("delegator prompt must NOT say 'OpenCode will create branches' — the CI framework handles that")
	}

	// Must have a dedicated rule forbidding branch/PR instructions in /opencode comments for code changes
	if !strings.Contains(prompt, "git checkout") || !strings.Contains(prompt, "gh pr") {
		t.Error("delegator prompt must explicitly list git checkout and gh pr as forbidden commands in /opencode comments")
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
