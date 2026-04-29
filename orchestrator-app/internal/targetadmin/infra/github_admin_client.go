package infra

import (
	"context"
	"fmt"

	githubdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/targetadmin/domain"
)

// GitHubAdminClient adapts the existing github.Client to satisfy
// targetadmin/domain.GitHubAdminClient. SetActionsSecret encapsulates the
// libsodium sealing step so the reconciler operates on plaintext only.
type GitHubAdminClient struct {
	gh *githubdomain.Client
}

func NewGitHubAdminClient(gh *githubdomain.Client) *GitHubAdminClient {
	return &GitHubAdminClient{gh: gh}
}

func (a *GitHubAdminClient) ListWebhooks(ctx context.Context, owner, repo, pat string) ([]domain.Webhook, error) {
	hooks, err := a.gh.ListWebhooks(ctx, owner, repo, pat)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Webhook, 0, len(hooks))
	for _, h := range hooks {
		out = append(out, domain.Webhook{
			ID:     h.ID,
			URL:    h.URL,
			Events: h.Events,
			Active: h.Active,
		})
	}
	return out, nil
}

func (a *GitHubAdminClient) CreateWebhook(ctx context.Context, owner, repo, pat string, w domain.Webhook) error {
	return a.gh.CreateWebhook(ctx, owner, repo, pat, githubdomain.Webhook{
		URL: w.URL, Secret: w.Secret, Events: w.Events, Active: w.Active,
	})
}

func (a *GitHubAdminClient) UpdateWebhook(ctx context.Context, owner, repo string, hookID int64, pat string, w domain.Webhook) error {
	return a.gh.UpdateWebhook(ctx, owner, repo, hookID, pat, githubdomain.Webhook{
		URL: w.URL, Secret: w.Secret, Events: w.Events, Active: w.Active,
	})
}

func (a *GitHubAdminClient) SetActionsSecret(ctx context.Context, owner, repo, name, plaintext, pat string) error {
	keyID, pubKey, err := a.gh.GetActionsSecretPublicKey(ctx, owner, repo, pat)
	if err != nil {
		return fmt.Errorf("fetch actions public key: %w", err)
	}
	encrypted, err := sealActionsSecret(plaintext, pubKey)
	if err != nil {
		return fmt.Errorf("seal secret: %w", err)
	}
	if err := a.gh.PutActionsSecret(ctx, owner, repo, name, encrypted, keyID, pat); err != nil {
		return fmt.Errorf("put actions secret: %w", err)
	}
	return nil
}
