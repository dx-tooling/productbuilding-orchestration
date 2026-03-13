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
	getPRDiffResult     string
	getPRDiffErr        error
	closeIssueErr          error
	closePRErr             error
	searchCodeResult       []CodeSearchResult
	searchCodeErr          error
	getFileContentsResult  *FileContents
	getFileContentsErr     error

	// Captured args
	lastCommentBody    string
	lastClosedIssue    int
	lastClosedPRNumber int
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

func (m *mockGitHubClient) GetPRDiff(_ context.Context, _, _ string, _ int, _ string) (string, error) {
	return m.getPRDiffResult, m.getPRDiffErr
}

func (m *mockGitHubClient) CloseIssue(_ context.Context, _, _ string, number int, _ string) error {
	m.lastClosedIssue = number
	return m.closeIssueErr
}

func (m *mockGitHubClient) ClosePR(_ context.Context, _, _ string, prNumber int, _ string) error {
	m.lastClosedPRNumber = prNumber
	return m.closePRErr
}

func (m *mockGitHubClient) SearchCode(_ context.Context, _, _, _, _ string) ([]CodeSearchResult, error) {
	return m.searchCodeResult, m.searchCodeErr
}

func (m *mockGitHubClient) GetFileContents(_ context.Context, _, _, _, _, _ string) (*FileContents, error) {
	return m.getFileContentsResult, m.getFileContentsErr
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

func TestToolExecutor_SearchPRDiff(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main
+import "fmt"
+func hello() { fmt.Println("Hello") }
diff --git a/utils.go b/utils.go
--- a/utils.go
+++ b/utils.go
@@ -1 +1,2 @@
 package main
+func helper() { fmt.Println("Helper") }
`
	gh := &mockGitHubClient{getPRDiffResult: diff}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_pr_diff",
			Arguments: `{"pr_number":10,"pattern":"fmt"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected result to contain filename, got: %s", result)
	}
	if !strings.Contains(result, "fmt") {
		t.Errorf("expected result to contain matching pattern, got: %s", result)
	}
}

func TestToolExecutor_SearchPRDiff_NoMatches(t *testing.T) {
	gh := &mockGitHubClient{getPRDiffResult: "diff --git a/main.go b/main.go\n+package main\n"}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_pr_diff",
			Arguments: `{"pr_number":10,"pattern":"nonexistent"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No lines matching") {
		t.Errorf("expected 'No lines matching' message, got: %s", result)
	}
}

func TestToolExecutor_CloseIssue(t *testing.T) {
	gh := &mockGitHubClient{}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "close_github_issue",
			Arguments: `{"number":7}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gh.lastClosedIssue != 7 {
		t.Errorf("expected issue 7 to be closed, got %d", gh.lastClosedIssue)
	}
	if !strings.Contains(result, "#7") {
		t.Errorf("expected result to contain issue number, got: %s", result)
	}
}

func TestToolExecutor_ClosePR(t *testing.T) {
	gh := &mockGitHubClient{}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "close_github_pr",
			Arguments: `{"pr_number":35}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gh.lastClosedPRNumber != 35 {
		t.Errorf("expected PR 35 to be closed, got %d", gh.lastClosedPRNumber)
	}
	if !strings.Contains(result, "#35") {
		t.Errorf("expected result to contain PR number, got: %s", result)
	}
}

func TestToolExecutor_SearchCode(t *testing.T) {
	gh := &mockGitHubClient{
		searchCodeResult: []CodeSearchResult{
			{Path: "internal/auth/handler.go", TextMatches: []string{"func Authenticate(ctx context.Context)"}},
			{Path: "cmd/server/main.go", TextMatches: []string{"auth.NewHandler(db)"}},
		},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_repo_code",
			Arguments: `{"query":"Authenticate"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "internal/auth/handler.go") {
		t.Errorf("expected file path in result, got: %s", result)
	}
	if !strings.Contains(result, "func Authenticate") {
		t.Errorf("expected code snippet in result, got: %s", result)
	}
}

func TestToolExecutor_SearchCode_NoResults(t *testing.T) {
	gh := &mockGitHubClient{searchCodeResult: []CodeSearchResult{}}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "search_repo_code",
			Arguments: `{"query":"nonexistent"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No code found") {
		t.Errorf("expected 'No code found' message, got: %s", result)
	}
}

func TestToolExecutor_GetFileContents_File(t *testing.T) {
	gh := &mockGitHubClient{
		getFileContentsResult: &FileContents{
			Path:    "main.go",
			Type:    "file",
			Size:    42,
			Content: "package main\n\nfunc main() {}\n",
		},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "get_file_contents",
			Arguments: `{"path":"main.go","ref":"feature-branch"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected file path in result, got: %s", result)
	}
	if !strings.Contains(result, "package main") {
		t.Errorf("expected file content in result, got: %s", result)
	}
	if !strings.Contains(result, "feature-branch") {
		t.Errorf("expected ref in result, got: %s", result)
	}
}

func TestToolExecutor_GetFileContents_Dir(t *testing.T) {
	gh := &mockGitHubClient{
		getFileContentsResult: &FileContents{
			Path: "internal/",
			Type: "dir",
			Entries: []DirEntry{
				{Name: "auth", Path: "internal/auth", Type: "dir"},
				{Name: "main.go", Path: "internal/main.go", Type: "file", Size: 256},
			},
		},
	}
	exec := NewToolExecutor(gh)

	result, err := exec.Execute(context.Background(), ToolCall{
		Function: FunctionCall{
			Name:      "get_file_contents",
			Arguments: `{"path":"internal/"}`,
		},
	}, testTarget)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Directory") {
		t.Errorf("expected directory listing, got: %s", result)
	}
	if !strings.Contains(result, "auth/") {
		t.Errorf("expected dir entry, got: %s", result)
	}
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected file entry, got: %s", result)
	}
}

func TestToolDefinitions_Count(t *testing.T) {
	defs := ToolDefinitions()
	if len(defs) != 10 {
		t.Errorf("expected 10 tool definitions, got %d", len(defs))
	}
}
