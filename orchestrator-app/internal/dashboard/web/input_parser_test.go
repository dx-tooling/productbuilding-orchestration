package web

import (
	"testing"
)

func TestParseInvestigationInput_IssueNumber(t *testing.T) {
	result, err := ParseInvestigationInput("#123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != QueryIssue {
		t.Fatalf("expected QueryIssue, got %v", result.Type)
	}
	if result.Number != 123 {
		t.Errorf("expected number 123, got %d", result.Number)
	}
}

func TestParseInvestigationInput_GitHubIssueURL(t *testing.T) {
	result, err := ParseInvestigationInput("https://github.com/luminor-project/luminor-core-go-playground/issues/57")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != QueryGitHub {
		t.Fatalf("expected QueryGitHub, got %v", result.Type)
	}
	if result.Owner != "luminor-project" {
		t.Errorf("expected owner 'luminor-project', got %q", result.Owner)
	}
	if result.Repo != "luminor-core-go-playground" {
		t.Errorf("expected repo 'luminor-core-go-playground', got %q", result.Repo)
	}
	if result.Number != 57 {
		t.Errorf("expected number 57, got %d", result.Number)
	}
}

func TestParseInvestigationInput_GitHubPRURL(t *testing.T) {
	result, err := ParseInvestigationInput("https://github.com/luminor-project/luminor-core-go-playground/pull/59")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != QueryGitHub {
		t.Fatalf("expected QueryGitHub, got %v", result.Type)
	}
	if result.Owner != "luminor-project" {
		t.Errorf("expected owner, got %q", result.Owner)
	}
	if result.Number != 59 {
		t.Errorf("expected number 59, got %d", result.Number)
	}
}

func TestParseInvestigationInput_SlackMessageURL(t *testing.T) {
	result, err := ParseInvestigationInput("https://luminor-tech.slack.com/archives/C0AL8824SBH/p1773605494857279")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != QuerySlack {
		t.Fatalf("expected QuerySlack, got %v", result.Type)
	}
	if result.SlackChannel != "C0AL8824SBH" {
		t.Errorf("expected channel 'C0AL8824SBH', got %q", result.SlackChannel)
	}
	// Slack p-timestamps: p1773605494857279 → "1773605494.857279"
	if result.SlackTs != "1773605494.857279" {
		t.Errorf("expected ts '1773605494.857279', got %q", result.SlackTs)
	}
}

func TestParseInvestigationInput_SlackThreadURL(t *testing.T) {
	result, err := ParseInvestigationInput("https://luminor-tech.slack.com/archives/C0AL8824SBH/p1773591330065709?thread_ts=1773578348.731309&cid=C0AL8824SBH")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Type != QuerySlack {
		t.Fatalf("expected QuerySlack, got %v", result.Type)
	}
	if result.SlackChannel != "C0AL8824SBH" {
		t.Errorf("expected channel, got %q", result.SlackChannel)
	}
	// When thread_ts is present, use it (it's the parent thread)
	if result.SlackThreadTs != "1773578348.731309" {
		t.Errorf("expected thread_ts '1773578348.731309', got %q", result.SlackThreadTs)
	}
}

func TestParseInvestigationInput_Invalid(t *testing.T) {
	_, err := ParseInvestigationInput("just some random text")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestParseInvestigationInput_Empty(t *testing.T) {
	_, err := ParseInvestigationInput("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}
