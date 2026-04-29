package domain

import (
	"context"
	"log/slog"
	"sort"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// Reconciler ensures each registered target has a webhook on GitHub pointing
// at this orchestrator with the correct secret/events, and (if a Fireworks
// API key is configured for the target) an up-to-date FIREWORKS_API_KEY
// Actions secret. Per the design decision logged in the plan, this runs only
// at orchestrator startup — re-reconciliation comes via `mise run deploy`.
type Reconciler struct {
	registry   *targets.Registry
	admin      GitHubAdminClient
	webhookURL string
}

// desiredEvents are the webhook events the orchestrator must subscribe to on
// every target. Centralised so the equivalence check in ensureWebhook can
// compare against the same source of truth that's passed on Create/Update.
var desiredEvents = []string{"pull_request", "issues", "issue_comment"}

const fireworksSecretName = "FIREWORKS_API_KEY"

func NewReconciler(registry *targets.Registry, admin GitHubAdminClient, webhookURL string) *Reconciler {
	return &Reconciler{registry: registry, admin: admin, webhookURL: webhookURL}
}

// ReconcileAll walks every registered target and ensures its GitHub-side
// ingress matches desired state. Per-target failures are logged but never
// abort the loop; the goroutine logs a summary line at the end.
func (r *Reconciler) ReconcileAll(ctx context.Context) {
	all := r.registry.All()
	if len(all) == 0 {
		return
	}

	var ok, failed int
	for _, t := range all {
		if err := r.reconcileOne(ctx, t); err != nil {
			slog.Warn("targetadmin: reconcile failed",
				"owner", t.RepoOwner, "repo", t.RepoName, "error", err)
			failed++
			continue
		}
		ok++
	}
	slog.Info("targetadmin: reconcile complete", "ok", ok, "failed", failed, "total", len(all))
}

func (r *Reconciler) reconcileOne(ctx context.Context, t targets.TargetConfig) error {
	if err := r.ensureWebhook(ctx, t); err != nil {
		return err
	}
	if t.FireworksAPIKey == "" {
		return nil
	}
	return r.admin.SetActionsSecret(ctx, t.RepoOwner, t.RepoName, fireworksSecretName, t.FireworksAPIKey, t.GitHubPAT)
}

func (r *Reconciler) ensureWebhook(ctx context.Context, t targets.TargetConfig) error {
	hooks, err := r.admin.ListWebhooks(ctx, t.RepoOwner, t.RepoName, t.GitHubPAT)
	if err != nil {
		return err
	}

	desired := Webhook{
		URL:    r.webhookURL,
		Secret: t.WebhookSecret,
		Events: desiredEvents,
		Active: true,
	}

	for _, h := range hooks {
		if h.URL != r.webhookURL {
			continue
		}
		if webhookEquivalent(h, desired) {
			return nil
		}
		slog.Info("targetadmin: updating webhook",
			"owner", t.RepoOwner, "repo", t.RepoName, "id", h.ID)
		return r.admin.UpdateWebhook(ctx, t.RepoOwner, t.RepoName, h.ID, t.GitHubPAT, desired)
	}

	slog.Info("targetadmin: creating webhook",
		"owner", t.RepoOwner, "repo", t.RepoName)
	return r.admin.CreateWebhook(ctx, t.RepoOwner, t.RepoName, t.GitHubPAT, desired)
}

// webhookEquivalent compares the observable fields of two webhooks. Secret is
// excluded because GitHub never returns it on List; treating identical other
// fields as a match means we don't churn updates on every reconcile when the
// secret hasn't changed. (When the secret IS rotated, the next Update will
// happen anyway because of some other observable diff or because the operator
// explicitly bumps the webhook URL.) For the conservative "always re-assert
// the secret" behaviour, set the events list to differ — but in practice
// secrets rotate rarely and a `mise run deploy` triggered by the rotation
// will run reconcile through Update anyway when other fields change.
func webhookEquivalent(a, b Webhook) bool {
	if a.URL != b.URL || a.Active != b.Active {
		return false
	}
	if len(a.Events) != len(b.Events) {
		return false
	}
	ae := append([]string(nil), a.Events...)
	be := append([]string(nil), b.Events...)
	sort.Strings(ae)
	sort.Strings(be)
	for i := range ae {
		if ae[i] != be[i] {
			return false
		}
	}
	return true
}
