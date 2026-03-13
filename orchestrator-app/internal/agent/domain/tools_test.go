package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// mockGitHubClient is a test double for GitHubClient.
type mockGitHubClient struct {
	createIssueResult   int
	createIssueErr      error
	createCommentResult int64
	createCommentErr    error
	searchResults       []IssueSearchResult
	searchErr           error
	getIssueResult      *IssueDetail
	getIssueErr         error
	listIssuesResult    []IssueDetail
	listIssuesErr       error

	// Captured args
	lastCommentBody string
}

func (m *mockGitHubClient) CreateIssue(_ context.Context, _, _, _, _, _ string) (int, error) {
	return m.createIssueResult, m.createIssueErr
}

func (m *mockGitHubClient) CreateComment(_ context.Context, _, _ string, _ int, body, _ string) (int64, error) {
	m.lastCommentBody = body
	return m.createCommentResult, m.createCommentErr
}

func (m *mockGitHubClient) SearchIssues(_ context.Context, _, _, _, _ string) ([]IssueSearchResult, error) {
	return m.searchResults, m.searchErr
}

func (m *mockGitHubClient) GetIssue(_ context.Context, _, _ string, _ int, _ string) (*IssueDetail, error) {
	return m.getIssueResult, m.getIssueErr
}

func (m *mockGitHubClient) ListIssues(_ context.Context, _, _, _, _ string, _ int) ([]IssueDetail, error) {
	return m.listIssuesResult, m.listIssuesErr
}

var testTarget = targets.TargetConfig{
	RepoOwner: "acme",
	RepoName:  "widgets",
	GitHubPAT: "pat-123",
}

func TestToolExecutor_CreateIssue(t *testing.T) {
	gh := &mockGitHubClient{createIssueResult: 42}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "create_github_issue",
			Arguments: `{"title":"Bug fix","body":"Fix the login"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "#42") {
		t.Errorf("expected result to contain issue number, got: %s", result)
	}
	if len(exec.Effects().CreatedIssues) != 1 {
		t.Errorf("expected 1 created issue, got %d", len(exec.Effects().CreatedIssues))
	}
	if exec.Effects().CreatedIssues[0].Number != 42 {
		t.Errorf("expected issue number 42, got %d", exec.Effects().CreatedIssues[0].Number)
	}
}

func TestToolExecutor_SearchIssues(t *testing.T) {
	gh := &mockGitHubClient{
		searchResults: []IssueSearchResult{
			{Number: 1, Title: "Login bug", State: "open"},
		},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_github_issues",
			Arguments: `{"query":"login"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Login bug") {
		t.Errorf("expected result to contain issue title, got: %s", result)
	}
}

func TestToolExecutor_SearchIssues_NoResults(t *testing.T) {
	gh := &mockGitHubClient{searchResults: []IssueSearchResult{}}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_github_issues",
			Arguments: `{"query":"nonexistent"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No issues found") {
		t.Errorf("expected 'No issues found' message, got: %s", result)
	}
}

func TestToolExecutor_AddComment_AppendsViaAgentMarker(t *testing.T) {
	gh := &mockGitHubClient{createCommentResult: 99}
	exec := NewToolExecutor(gh)

	_, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "add_github_comment",
			Arguments: `{"number":5,"body":"/opencode Plan this"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gh.lastCommentBody, "<!-- via-agent -->") {
		t.Errorf("expected via-agent marker in comment body, got: %s", gh.lastCommentBody)
	}
	if len(exec.Effects().PostedComments) != 1 || exec.Effects().PostedComments[0] != 99 {
		t.Errorf("expected posted comment ID 99")
	}
}

func TestToolExecutor_GetIssue(t *testing.T) {
	gh := &mockGitHubClient{
		getIssueResult: &IssueDetail{Number: 5, Title: "Test issue", State: "open"},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "get_github_issue",
			Arguments: `{"number":5}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Test issue") {
		t.Errorf("expected result to contain issue title, got: %s", result)
	}
}

func TestToolExecutor_ListIssues(t *testing.T) {
	gh := &mockGitHubClient{
		listIssuesResult: []IssueDetail{
			{Number: 1, Title: "First", State: "open"},
			{Number: 2, Title: "Second", State: "open"},
		},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "list_github_issues",
			Arguments: `{}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "First") || !strings.Contains(result, "Second") {
		t.Errorf("expected result to list issues, got: %s", result)
	}
}

func TestToolExecutor_UnknownTool(t *testing.T) {
	exec := NewToolExecutor(&mockGitHubClient{})

	_, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{Name: "unknown_tool", Arguments: "{}"},
	}, testTarget)

	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestToolExecutor_GitHubError(t *testing.T) {
	gh := &mockGitHubClient{createIssueErr: fmt.Errorf("network error")}
	exec := NewToolExecutor(gh)

	_, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "create_github_issue",
			Arguments: `{"title":"Bug","body":"Details"}`,
		},
	}, testTarget)

	if err == nil {
		t.Fatal("expected error from GitHub client")
	}
	if !strings.Contains(err.Error(), "network error") {
		t.Errorf("expected 'network error', got: %v", err)
	}
}

func TestToolDefinitions_Count(t *testing.T) {
	defs := ToolDefinitions()
	if len(defs) != 5 {
		t.Errorf("expected 5 tool definitions, got %d", len(defs))
	}
}
