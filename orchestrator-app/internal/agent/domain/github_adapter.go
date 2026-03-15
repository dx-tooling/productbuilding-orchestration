package domain

import (
	"context"

	githubdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
)

// GitHubClientAdapter wraps a github domain Client to satisfy the agent's GitHubClient interface.
type GitHubClientAdapter struct {
	client *githubdomain.Client
}

// NewGitHubClientAdapter creates a new adapter.
func NewGitHubClientAdapter(client *githubdomain.Client) *GitHubClientAdapter {
	return &GitHubClientAdapter{client: client}
}

func (a *GitHubClientAdapter) CreateIssue(ctx context.Context, owner, repo, title, body, pat string) (int, error) {
	return a.client.CreateIssue(ctx, owner, repo, title, body, pat)
}

func (a *GitHubClientAdapter) CreateComment(ctx context.Context, owner, repo string, number int, body, pat string) (int64, error) {
	return a.client.CreateComment(ctx, owner, repo, number, body, pat)
}

func (a *GitHubClientAdapter) SearchIssues(ctx context.Context, owner, repo, query, pat string) ([]IssueSearchResult, error) {
	results, err := a.client.SearchIssues(ctx, owner, repo, query, pat)
	if err != nil {
		return nil, err
	}
	out := make([]IssueSearchResult, len(results))
	for i, r := range results {
		out[i] = IssueSearchResult{
			Number: r.Number,
			Title:  r.Title,
			State:  r.State,
			URL:    r.URL,
		}
	}
	return out, nil
}

func (a *GitHubClientAdapter) GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueDetail, error) {
	result, err := a.client.GetIssue(ctx, owner, repo, number, pat)
	if err != nil {
		return nil, err
	}
	return &IssueDetail{
		Number: result.Number,
		Title:  result.Title,
		Body:   result.Body,
		State:  result.State,
		URL:    result.URL,
		User:   result.User,
		Labels: result.Labels,
	}, nil
}

func (a *GitHubClientAdapter) ListIssues(ctx context.Context, owner, repo, state, pat string, limit int) ([]IssueDetail, error) {
	results, err := a.client.ListIssues(ctx, owner, repo, state, pat, limit)
	if err != nil {
		return nil, err
	}
	out := make([]IssueDetail, len(results))
	for i, r := range results {
		out[i] = IssueDetail{
			Number: r.Number,
			Title:  r.Title,
			Body:   r.Body,
			State:  r.State,
			URL:    r.URL,
			User:   r.User,
			Labels: r.Labels,
		}
	}
	return out, nil
}

func (a *GitHubClientAdapter) GetPRDiff(ctx context.Context, owner, repo string, prNumber int, pat string) (string, error) {
	return a.client.GetPRDiff(ctx, owner, repo, prNumber, pat)
}

func (a *GitHubClientAdapter) CloseIssue(ctx context.Context, owner, repo string, number int, pat string) error {
	return a.client.CloseIssue(ctx, owner, repo, number, pat)
}

func (a *GitHubClientAdapter) ClosePR(ctx context.Context, owner, repo string, prNumber int, pat string) error {
	return a.client.ClosePR(ctx, owner, repo, prNumber, pat)
}

func (a *GitHubClientAdapter) SearchCode(ctx context.Context, owner, repo, query, pat string) ([]CodeSearchResult, error) {
	results, err := a.client.SearchCode(ctx, owner, repo, query, pat)
	if err != nil {
		return nil, err
	}
	out := make([]CodeSearchResult, len(results))
	for i, r := range results {
		out[i] = CodeSearchResult{
			Path:        r.Path,
			HTMLURL:     r.HTMLURL,
			TextMatches: r.TextMatches,
		}
	}
	return out, nil
}

func (a *GitHubClientAdapter) ListWorkflowRuns(ctx context.Context, owner, repo, branch, pat string, limit int) ([]WorkflowRun, error) {
	results, err := a.client.ListWorkflowRuns(ctx, owner, repo, branch, pat, limit)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowRun, len(results))
	for i, r := range results {
		out[i] = WorkflowRun{
			ID:         r.ID,
			Name:       r.Name,
			Status:     r.Status,
			Conclusion: r.Conclusion,
			HTMLURL:    r.HTMLURL,
			HeadBranch: r.HeadBranch,
			Event:      r.Event,
			CreatedAt:  r.CreatedAt,
			UpdatedAt:  r.UpdatedAt,
		}
	}
	return out, nil
}

func (a *GitHubClientAdapter) ListWorkflowRunJobs(ctx context.Context, owner, repo string, runID int64, pat string) ([]WorkflowRunJob, error) {
	results, err := a.client.ListWorkflowRunJobs(ctx, owner, repo, runID, pat)
	if err != nil {
		return nil, err
	}
	out := make([]WorkflowRunJob, len(results))
	for i, j := range results {
		steps := make([]WorkflowRunStep, len(j.Steps))
		for k, s := range j.Steps {
			steps[k] = WorkflowRunStep{
				Name:       s.Name,
				Status:     s.Status,
				Conclusion: s.Conclusion,
				Number:     s.Number,
			}
		}
		out[i] = WorkflowRunJob{
			ID:         j.ID,
			Name:       j.Name,
			Status:     j.Status,
			Conclusion: j.Conclusion,
			HTMLURL:    j.HTMLURL,
			Steps:      steps,
		}
	}
	return out, nil
}

func (a *GitHubClientAdapter) GetJobLogs(ctx context.Context, owner, repo string, jobID int64, pat string) (string, error) {
	return a.client.GetJobLogs(ctx, owner, repo, jobID, pat)
}

func (a *GitHubClientAdapter) GetFileContents(ctx context.Context, owner, repo, path, ref, pat string) (*FileContents, error) {
	result, err := a.client.GetFileContents(ctx, owner, repo, path, ref, pat)
	if err != nil {
		return nil, err
	}
	fc := &FileContents{
		Path:    result.Path,
		Type:    result.Type,
		Size:    result.Size,
		Content: result.Content,
	}
	if len(result.Entries) > 0 {
		fc.Entries = make([]DirEntry, len(result.Entries))
		for i, e := range result.Entries {
			fc.Entries[i] = DirEntry{Name: e.Name, Path: e.Path, Type: e.Type, Size: e.Size}
		}
	}
	return fc, nil
}
