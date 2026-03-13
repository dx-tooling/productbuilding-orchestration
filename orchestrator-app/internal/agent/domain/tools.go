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
	SearchCode(ctx context.Context, owner, repo, query, pat string) ([]CodeSearchResult, error)
	GetFileContents(ctx context.Context, owner, repo, path, ref, pat string) (*FileContents, error)
}

// CodeSearchResult is returned by SearchCode.
type CodeSearchResult struct {
	Path        string   `json:"path"`
	HTMLURL     string   `json:"html_url"`
	TextMatches []string `json:"text_matches"`
}

// FileContents is returned by GetFileContents.
type FileContents struct {
	Path    string     `json:"path"`
	Type    string     `json:"type"`
	Size    int        `json:"size"`
	Content string     `json:"content,omitempty"`
	Entries []DirEntry `json:"entries,omitempty"`
}

// DirEntry represents an entry in a directory listing.
type DirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
	Size int    `json:"size"`
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
	ResetEffects()
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

// ResetEffects clears accumulated side effects. Called at the start of each agent run.
func (e *GitHubToolExecutor) ResetEffects() {
	e.effects = SideEffects{}
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
	case "search_repo_code":
		return e.searchCode(ctx, call.Function.Arguments, target)
	case "get_file_contents":
		return e.getFileContents(ctx, call.Function.Arguments, target)
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

func (e *GitHubToolExecutor) searchCode(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	results, err := e.github.SearchCode(ctx, target.RepoOwner, target.RepoName, args.Query, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return fmt.Sprintf("No code found matching %q.", args.Query), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d file(s) matching %q:\n\n", len(results), args.Query))
	for _, r := range results {
		sb.WriteString(r.Path + "\n")
		for _, fragment := range r.TextMatches {
			for _, line := range strings.Split(fragment, "\n") {
				sb.WriteString("  " + line + "\n")
			}
			sb.WriteString("\n")
		}
	}
	return sb.String(), nil
}

func (e *GitHubToolExecutor) getFileContents(ctx context.Context, argsJSON string, target targets.TargetConfig) (string, error) {
	var args struct {
		Path string `json:"path"`
		Ref  string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}

	fc, err := e.github.GetFileContents(ctx, target.RepoOwner, target.RepoName, args.Path, args.Ref, target.GitHubPAT)
	if err != nil {
		return "", err
	}

	if fc.Type == "dir" {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Directory: %s (%d entries)\n\n", fc.Path, len(fc.Entries)))
		for _, entry := range fc.Entries {
			if entry.Type == "dir" {
				sb.WriteString(fmt.Sprintf("  dir   %s/\n", entry.Name))
			} else {
				sb.WriteString(fmt.Sprintf("  file  %s  (%d bytes)\n", entry.Name, entry.Size))
			}
		}
		return sb.String(), nil
	}

	// File: cap at 500 lines to avoid blowing up context
	lines := strings.Split(fc.Content, "\n")
	truncated := false
	if len(lines) > 500 {
		lines = lines[:500]
		truncated = true
	}

	ref := args.Ref
	if ref == "" {
		ref = "default branch"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("File: %s (%d bytes, ref: %s)\n\n", fc.Path, fc.Size, ref))
	for i, line := range lines {
		sb.WriteString(fmt.Sprintf("%4d  %s\n", i+1, line))
	}
	if truncated {
		sb.WriteString(fmt.Sprintf("\n... truncated (showing 500 of %d lines)\n", len(strings.Split(fc.Content, "\n"))))
	}

	return sb.String(), nil
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
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "search_repo_code",
				Description: "Search for code in the repository by keyword or pattern. Returns matching files with code snippets. Searches the default branch.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {"type": "string", "description": "Code search query (e.g. 'handleAuth', 'func NewServer', 'TODO fix')"}
					},
					"required": ["query"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "get_file_contents",
				Description: "Read the contents of a file or list a directory in the repository. Can read from any branch, tag, or commit SHA.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "description": "File or directory path (e.g. 'src/main.go', 'internal/auth/')"},
						"ref": {"type": "string", "description": "Branch name, tag, or commit SHA (default: repository default branch)"}
					},
					"required": ["path"]
				}`),
			},
		},
		{
			Type: "function",
			Function: ToolSchema{
				Name:        "list_conversations",
				Description: "List recent bot conversations in the current Slack channel. Use when asked about past discussions or conversation history.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"days": {"type": "integer", "description": "Number of days to look back (default: 14)"}
					}
				}`),
			},
		},
	}
}
