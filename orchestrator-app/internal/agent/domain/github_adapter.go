package domain

import (
	"context"

	githubdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/domain"
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
