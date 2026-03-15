package domain

import (
	"testing"
)

func toolNames(defs []ToolDef) []string {
	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	return names
}

func containsTool(defs []ToolDef, name string) bool {
	for _, d := range defs {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}

func TestIssueCreatorTools_ReturnsOnlySearchAndCreate(t *testing.T) {
	defs := IssueCreatorTools()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(defs), toolNames(defs))
	}
	if !containsTool(defs, "search_github_issues") {
		t.Error("missing search_github_issues")
	}
	if !containsTool(defs, "create_github_issue") {
		t.Error("missing create_github_issue")
	}
}

func TestDelegatorTools_ReturnsOnlyGetAndComment(t *testing.T) {
	defs := DelegatorTools()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(defs), toolNames(defs))
	}
	if !containsTool(defs, "get_github_issue") {
		t.Error("missing get_github_issue")
	}
	if !containsTool(defs, "add_github_comment") {
		t.Error("missing add_github_comment")
	}
}

func TestCommenterTools_ReturnsOnlyGetAndComment(t *testing.T) {
	defs := CommenterTools()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(defs), toolNames(defs))
	}
	if !containsTool(defs, "get_github_issue") {
		t.Error("missing get_github_issue")
	}
	if !containsTool(defs, "add_github_comment") {
		t.Error("missing add_github_comment")
	}
}

func TestResearcherTools_ReturnsAllReadOnlyTools(t *testing.T) {
	defs := ResearcherTools()
	if len(defs) != 10 {
		t.Fatalf("expected 10 tools, got %d: %v", len(defs), toolNames(defs))
	}
	expected := []string{
		"search_github_issues",
		"get_github_issue",
		"list_github_issues",
		"search_pr_diff",
		"search_repo_code",
		"get_file_contents",
		"list_conversations",
		"list_workflow_runs",
		"get_workflow_run_jobs",
		"get_job_failure_context",
	}
	for _, name := range expected {
		if !containsTool(defs, name) {
			t.Errorf("missing %s", name)
		}
	}
}

func TestCloserTools_ReturnsOnlyGetAndClose(t *testing.T) {
	defs := CloserTools()
	if len(defs) != 3 {
		t.Fatalf("expected 3 tools, got %d: %v", len(defs), toolNames(defs))
	}
	if !containsTool(defs, "get_github_issue") {
		t.Error("missing get_github_issue")
	}
	if !containsTool(defs, "close_github_issue") {
		t.Error("missing close_github_issue")
	}
	if !containsTool(defs, "close_github_pr") {
		t.Error("missing close_github_pr")
	}
}

func TestAllSpecialistToolsAreSubsetsOfFullDefinitions(t *testing.T) {
	full := ToolDefinitions()
	fullNames := make(map[string]bool)
	for _, d := range full {
		fullNames[d.Function.Name] = true
	}

	subsets := map[string][]ToolDef{
		"issue_creator": IssueCreatorTools(),
		"delegator":     DelegatorTools(),
		"commenter":     CommenterTools(),
		"researcher":    ResearcherTools(),
		"closer":        CloserTools(),
	}

	for specialist, defs := range subsets {
		for _, d := range defs {
			if !fullNames[d.Function.Name] {
				t.Errorf("%s has tool %q which is not in ToolDefinitions()", specialist, d.Function.Name)
			}
		}
	}
}
