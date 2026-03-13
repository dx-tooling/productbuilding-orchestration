package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// GitHubClient defines the GitHub operations available to the agent tools.
type GitHubClient interface {
	CreateIssue(ctx context.Context, owner, repo, title, body, pat string) (int, error)
	CreateComment(ctx context.Context, owner, repo string, number int, body, pat string) (int64, error)
	SearchIssues(ctx context.Context, owner, repo, query, pat string) ([]IssueSearchResult, error)
	GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueDetail, error)
	ListIssues(ctx context.Context, owner, repo, state, pat string, limit int) ([]IssueDetail, error)
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

// parseIntArg is a helper for extracting int params from JSON args.
func parseIntArg(s string) (int, error) {
	return strconv.Atoi(s)
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
	}
}
