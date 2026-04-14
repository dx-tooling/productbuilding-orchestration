package featurecontext

import (
	"context"

	githubdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

// githubClient defines the GitHub client methods used by the adapters.
type githubClient interface {
	GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*githubdomain.IssueDetail, error)
	GetPR(ctx context.Context, owner, repo string, number int, pat string) (*githubdomain.PRDetail, error)
}

// previewRepository defines the preview repo method used by the adapter.
type previewRepository interface {
	FindByRepoPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*previewdomain.Preview, error)
}

// GitHubIssueAdapter adapts the GitHub client to the IssueGetter interface.
type GitHubIssueAdapter struct {
	client githubClient
}

func NewGitHubIssueAdapter(client githubClient) *GitHubIssueAdapter {
	return &GitHubIssueAdapter{client: client}
}

func (a *GitHubIssueAdapter) GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueState, error) {
	issue, err := a.client.GetIssue(ctx, owner, repo, number, pat)
	if err != nil {
		return nil, err
	}
	return &IssueState{
		Number: issue.Number,
		Title:  issue.Title,
		Body:   issue.Body,
		State:  issue.State,
	}, nil
}

// GitHubPRAdapter adapts the GitHub client to the PRGetter interface.
type GitHubPRAdapter struct {
	client githubClient
}

func NewGitHubPRAdapter(client githubClient) *GitHubPRAdapter {
	return &GitHubPRAdapter{client: client}
}

func (a *GitHubPRAdapter) GetPR(ctx context.Context, owner, repo string, number int, pat string) (*PRState, error) {
	pr, err := a.client.GetPR(ctx, owner, repo, number, pat)
	if err != nil {
		return nil, err
	}
	return &PRState{
		Number:    pr.Number,
		Title:     pr.Title,
		State:     pr.State,
		Merged:    pr.Merged,
		HeadSHA:   pr.HeadSHA,
		HeadRef:   pr.HeadRef,
		Author:    pr.User,
		Additions: pr.Additions,
		Deletions: pr.Deletions,
		URL:       pr.URL,
	}, nil
}

// actionsClient defines the GitHub client method used by the Actions-based adapter.
type actionsClient interface {
	ListWorkflowRunsForSHA(ctx context.Context, owner, repo, sha, pat string) ([]githubdomain.WorkflowRun, error)
}

// ActionsCheckRunAdapter implements CheckRunGetter using the GitHub Actions API
// instead of the Checks API. This works with fine-grained PATs (Actions: Read).
type ActionsCheckRunAdapter struct {
	client actionsClient
}

func NewActionsCheckRunAdapter(client actionsClient) *ActionsCheckRunAdapter {
	return &ActionsCheckRunAdapter{client: client}
}

func (a *ActionsCheckRunAdapter) GetCheckRunsForRef(ctx context.Context, owner, repo, ref, pat string) ([]CheckRunState, error) {
	runs, err := a.client.ListWorkflowRunsForSHA(ctx, owner, repo, ref, pat)
	if err != nil {
		return nil, err
	}
	states := make([]CheckRunState, len(runs))
	for i, r := range runs {
		states[i] = CheckRunState{
			Name:       r.Name,
			Conclusion: r.Conclusion,
			URL:        r.HTMLURL,
		}
	}
	return states, nil
}

// PreviewAdapter adapts the preview repository to the PreviewGetter interface.
type PreviewAdapter struct {
	repo previewRepository
}

func NewPreviewAdapter(repo previewRepository) *PreviewAdapter {
	return &PreviewAdapter{repo: repo}
}

func (a *PreviewAdapter) GetPreview(ctx context.Context, owner, repo string, prNumber int) (*PreviewState, error) {
	preview, err := a.repo.FindByRepoPR(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}
	if preview == nil {
		return nil, nil
	}
	return &PreviewState{
		Status: string(preview.Status),
		URL:    preview.PreviewURL,
	}, nil
}
