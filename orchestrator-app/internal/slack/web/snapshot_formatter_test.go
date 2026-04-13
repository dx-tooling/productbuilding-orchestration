package web

import (
	"strings"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
)

func TestFormatFeatureSummary_Full(t *testing.T) {
	snap := &featurecontext.FeatureSnapshot{
		Issue:    &featurecontext.IssueState{Number: 42, Title: "Add dark mode", State: "open"},
		PR:       &featurecontext.PRState{Number: 10, Author: "alice", State: "open", Additions: 50, Deletions: 10, HeadRef: "feature-branch"},
		CIStatus: featurecontext.CIFailing,
		CIDetails: []featurecontext.CheckRunState{
			{Name: "build", Conclusion: "success"},
			{Name: "lint", Conclusion: "failure"},
		},
		Preview: &featurecontext.PreviewState{Status: "ready", URL: "https://preview.example.com"},
	}

	result := FormatFeatureSummary(snap)

	if !strings.Contains(result, "Issue: #42") {
		t.Errorf("Expected issue line, got: %s", result)
	}
	if !strings.Contains(result, "PR: #10") {
		t.Errorf("Expected PR line, got: %s", result)
	}
	if !strings.Contains(result, "CI:") {
		t.Errorf("Expected CI line, got: %s", result)
	}
	if !strings.Contains(result, "build success") {
		t.Errorf("Expected 'build success' in CI details, got: %s", result)
	}
	if !strings.Contains(result, "lint failure") {
		t.Errorf("Expected 'lint failure' in CI details, got: %s", result)
	}
	if !strings.Contains(result, "Preview: ready") {
		t.Errorf("Expected preview line, got: %s", result)
	}
	if !strings.Contains(result, "preview.example.com") {
		t.Errorf("Expected preview URL, got: %s", result)
	}
}

func TestFormatFeatureSummary_IssueOnly(t *testing.T) {
	snap := &featurecontext.FeatureSnapshot{
		Issue:    &featurecontext.IssueState{Number: 42, Title: "Bug report", State: "open"},
		CIStatus: featurecontext.CIUnknown,
	}

	result := FormatFeatureSummary(snap)

	if !strings.Contains(result, "Issue: #42") {
		t.Errorf("Expected issue line, got: %s", result)
	}
	if strings.Contains(result, "PR:") {
		t.Errorf("Should not have PR line, got: %s", result)
	}
	if strings.Contains(result, "CI:") {
		t.Errorf("Should not have CI line when unknown, got: %s", result)
	}
	if strings.Contains(result, "Preview:") {
		t.Errorf("Should not have Preview line, got: %s", result)
	}
}

func TestFormatFeatureSummary_Nil(t *testing.T) {
	result := FormatFeatureSummary(nil)
	if result != "" {
		t.Errorf("Expected empty string for nil snapshot, got: %s", result)
	}
}
