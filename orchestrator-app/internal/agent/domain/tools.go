package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// GitHubClient defines the GitHub operations available to the agent tools.
type GitHubClient interface {
	CreateIssue(ctx context.Context, owner, repo, title, body, pat string) (int, error)
	CreateComment(ctx context.Context, owner, repo string, number int, body, pat string) (int64, error)
	SearchIssues(ctx context.Context, owner, repo, query, pat string) ([]IssueSearchResult, error)
	GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueDetail, error)
	ListIssues(ctx context.Context, owner, repo, state, pat string, limit int) ([]IssueDetail, error)
	GetPRDiff(ctx context.Context, owner, repo string, prNumber int, pat string) (string, error)
	CloseIssue(ctx context.Context, owner, repo string, number int, pat string) error
	ClosePR(ctx context.Context, owner, repo string, prNumber int, pat string) error
}

// IssueSearchResult is returned by SearchIssues.
type IssueSearchResult struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"html_url"`
}

// IssueDetail is returned by GetIssue and ListIssues.
type IssueDetail struct {
	Number int      `json:"number"`
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	State  string   `json:"state"`
	URL    string   `json:"html_url"`
	User   string   `json:"user"`
	Labels []string `json:"labels"`
}

// ToolExecutor executes tool calls requested by the LLM.
type ToolExecutor interface {
	Execute(ctx context.Context, call ToolCall, target targets.TargetConfig) (string, error)
	Effects() SideEffects
}

// GitHubToolExecutor executes agent tools against the GitHub API.
type GitHubToolExecutor struct {
	github  GitHubClient
	effects SideEffects
}

// NewToolExecutor creates a new tool executor backed by the given GitHub client.
func NewToolExecutor(github GitHubClient) *GitHubToolExecutor {
	return &GitHubToolExecutor{github: github}
}

// Effects returns the side effects accumulated during execution.
func (e *GitHubToolExecutor) Effects() SideEffects {
	return e.effects
}

// Execute dispatches a tool call to the appropriate handler.
func (e *GitHubToolExecutor) Execute(ctx context.Context, call ToolCall, target targets.TargetConfig) (string, error) {
	switch call.Function.Name {
	case "create_github_issue":
		return e.createIssue(ctx, call.Function.Arguments, target)
	case "search_github_issues":
		return e.searchIssues(ctx, call.Function.Arguments, target)
	case "add_github_comment":
		return e.addComment(ctx, call.Function.Arguments, target)
	case "get_github_issue":
		return e.getIssue(ctx, call.Function.Arguments, target)
	case "list_github_issues":
		return e.listIssues(ctx, call.Function.Arguments, target)
	case "search_pr_diff":
		return e.searchPRDiff(ctx, call.Function.Arguments, target)
	case "close_github_issue":
		return e.closeIssue(ctx, call.Function.Arguments, target)
	case "close_github_pr":
		return e.closePR(ctx, call.Function.Arguments, target)
	default:
		return "", fmt.Errorf("unknown tool: %s", call.Function.Name)
	}
}

func (e *GitHubToolExecutor) createIssue(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	number, err := e.github.CreateIssue(ctx, target.RepoOwner, target.RepoName, args.Title, args.Body, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	e.effects.CreatedIssues = append(e.effects.CreatedIssues, CreatedIssue{Number: number, Title: args.Title})

	return fmt.Sprintf("Created issue #%d: %s\nURL: https://github.com/%s/%s/issues/%d",
		number, args.Title, target.RepoOwner, target.RepoName, number), nil
}

func (e *GitHubToolExecutor) searchIssues(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	results, err := e.github.SearchIssues(ctx, target.RepoOwner, target.RepoName, args.Query, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No issues found matching the query.", nil
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return string(out), nil
}

func (e *GitHubToolExecutor) addComment(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Number int    `json:"number"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	body := args.Body + "\n\n<!-- via-agent -->"
	commentID, err := e.github.CreateComment(ctx, target.RepoOwner, target.RepoName, args.Number, body, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	e.effects.PostedComments = append(e.effects.PostedComments, commentID)
	commentURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d",
		target.RepoOwner, target.RepoName, args.Number, commentID)
	return fmt.Sprintf("Comment added to issue #%d.\nComment URL: %s", args.Number, commentURL), nil
}

func (e *GitHubToolExecutor) getIssue(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	issue, err := e.github.GetIssue(ctx, target.RepoOwner, target.RepoName, args.Number, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	out, _ := json.MarshalIndent(issue, "", "  ")
	return string(out), nil
}

func (e *GitHubToolExecutor) listIssues(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		State string `json:"state"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}
	if args.State == "" {
		args.State = "open"
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}

	issues, err := e.github.ListIssues(ctx, target.RepoOwner, target.RepoName, args.State, target.GitHubPAT, args.Limit)
	if err != nil {
		return "", err
	}

	if len(issues) == 0 {
		return "No " + args.State + " issues found.", nil
	}

	// Return compact summary
	var lines []string
	for _, issue := range issues {
		lines = append(lines, fmt.Sprintf("#%d [%s] %s", issue.Number, issue.State, issue.Title))
	}
	result, _ := json.MarshalIndent(lines, "", "  ")
	return string(result), nil
}

func (e *GitHubToolExecutor) searchPRDiff(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		PRNumber int    `json:"pr_number"`
		Pattern  string `json:"pattern"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	diff, err := e.github.GetPRDiff(ctx, target.RepoOwner, target.RepoName, args.PRNumber, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	pattern := strings.ToLower(args.Pattern)
	var matches []string
	var currentFile string

	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			// Extract filename from "diff --git a/path b/path"
			parts := strings.SplitN(line, " b/", 2)
			if len(parts) == 2 {
				currentFile = parts[1]
			}
		}
		if strings.Contains(strings.ToLower(line), pattern) {
			prefix := ""
			if currentFile != "" {
				prefix = currentFile + ": "
			}
			matches = append(matches, prefix+line)
		}
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No lines matching %q found in PR #%d diff.", args.Pattern, args.PRNumber), nil
	}

	// Cap output to avoid blowing up the context
	if len(matches) > 50 {
		matches = append(matches[:50], fmt.Sprintf("... and %d more matches", len(matches)-50))
	}

	return strings.Join(matches, "\n"), nil
}

func (e *GitHubToolExecutor) closeIssue(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	if err := e.github.CloseIssue(ctx, target.RepoOwner, target.RepoName, args.Number, target.GitHubPAT); err != nil {
		return "", err
	}

	return fmt.Sprintf("Closed issue #%d.\nURL: https://github.com/%s/%s/issues/%d",
		args.Number, target.RepoOwner, target.RepoName, args.Number), nil
}

func (e *GitHubToolExecutor) closePR(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		PRNumber int `json:"pr_number"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	if err := e.github.ClosePR(ctx, target.RepoOwner, target.RepoName, args.PRNumber, target.GitHubPAT); err != nil {
		return "", err
	}

	return fmt.Sprintf("Closed PR #%d.\nURL: https://github.com/%s/%s/pull/%d",
		args.PRNumber, target.RepoOwner, target.RepoName, args.PRNumber), nil
}

// ToolDefinitions returns the tool schemas to pass to the LLM.
func ToolDefinitions() []ToolDef {
	return []ToolDef{
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "create_github_issue",
				Description: "Create a new GitHub issue in the project repository.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"title": {"type": "string", "description": "Issue title"},
						"body": {"type": "string", "description": "Issue body (markdown)"}
					},
					"required": ["title", "body"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "search_github_issues",
				Description: "Search for existing GitHub issues by keyword query.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Search query keywords"}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "add_github_comment",
				Description: "Add a comment to an existing GitHub issue. Use '/opencode ...' prefix to trigger the AI coding agent.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"number": {"type": "integer", "description": "Issue number"},
						"body": {"type": "string", "description": "Comment body (markdown)"}
					},
					"required": ["number", "body"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "get_github_issue",
				Description: "Get details of a specific GitHub issue by number.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"number": {"type": "integer", "description": "Issue number"}
					},
					"required": ["number"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "list_github_issues",
				Description: "List GitHub issues in the repository.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"state": {"type": "string", "description": "Filter by state: open, closed, or all", "default": "open"},
						"limit": {"type": "integer", "description": "Maximum number of issues to return", "default": 10}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "search_pr_diff",
				Description: "Search through a pull request's code changes (diff) for lines matching a pattern. Use this to answer questions about code in a PR.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"pr_number": {"type": "integer", "description": "Pull request number"},
						"pattern": {"type": "string", "description": "Case-insensitive search pattern to match against diff lines"}
					},
					"required": ["pr_number", "pattern"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "close_github_issue",
				Description: "Close a GitHub issue.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"number": {"type": "integer", "description": "Issue number to close"}
					},
					"required": ["number"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "close_github_pr",
				Description: "Close a GitHub pull request.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"pr_number": {"type": "integer", "description": "Pull request number to close"}
					},
					"required": ["pr_number"]
				}`),
			},
		},
	}
}
