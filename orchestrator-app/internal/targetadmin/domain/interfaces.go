package domain

import "context"

// Webhook is the desired or observed configuration of a per-repo webhook on
// GitHub. Mirrors github.Webhook structurally so the targetadmin layer can
// stay independent of github/domain at the type level (the infra adapter
// translates).
//
// Secret is write-only on the GitHub side: it can be set on Create/Update
// but List/Get never return it. Treat empty Secret on a returned Webhook
// as "unknown", not "unset".
type Webhook struct {
	ID     int64
	URL    string
	Secret string
	Events []string
	Active bool
}

// GitHubAdminClient is the narrow GitHub API surface the reconciler needs.
// Implementations must use the per-target PAT supplied with each call so
// the caller naturally gets per-org isolation; no shared "management" PAT
// is involved.
type GitHubAdminClient interface {
	ListWebhooks(ctx context.Context, owner, repo, pat string) ([]Webhook, error)
	CreateWebhook(ctx context.Context, owner, repo, pat string, w Webhook) error
	UpdateWebhook(ctx context.Context, owner, repo string, hookID int64, pat string, w Webhook) error
	// SetActionsSecret writes (creates or updates) a repository-level Actions
	// secret. Implementations are responsible for fetching the repo's public
	// key and sealing the plaintext per GitHub's libsodium scheme.
	SetActionsSecret(ctx context.Context, owner, repo, name, plaintext, pat string) error
}
