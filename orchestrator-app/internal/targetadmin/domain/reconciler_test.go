package domain

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// ── Mock GitHubAdminClient ─────────────────────────────────────────────────

type listWebhooksCall struct{ Owner, Repo, PAT string }
type createWebhookCall struct {
	Owner, Repo, PAT string
	Hook             Webhook
}
type updateWebhookCall struct {
	Owner, Repo, PAT string
	HookID           int64
	Hook             Webhook
}
type setActionsSecretCall struct {
	Owner, Repo, Name, Plaintext, PAT string
}

type mockAdminClient struct {
	mu sync.Mutex

	listCalls   []listWebhooksCall
	createCalls []createWebhookCall
	updateCalls []updateWebhookCall
	secretCalls []setActionsSecretCall

	// Configurable: per-(owner/repo) pre-existing hooks returned by ListWebhooks.
	hooksByRepo map[string][]Webhook
	// Configurable: errors keyed by repo for fault-injection per scenario.
	listErr   map[string]error
	createErr map[string]error
	updateErr map[string]error
	secretErr map[string]error
}

func newMockAdmin() *mockAdminClient {
	return &mockAdminClient{
		hooksByRepo: make(map[string][]Webhook),
		listErr:     make(map[string]error),
		createErr:   make(map[string]error),
		updateErr:   make(map[string]error),
		secretErr:   make(map[string]error),
	}
}

func (m *mockAdminClient) key(owner, repo string) string { return owner + "/" + repo }

func (m *mockAdminClient) ListWebhooks(_ context.Context, owner, repo, pat string) ([]Webhook, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls = append(m.listCalls, listWebhooksCall{owner, repo, pat})
	if err := m.listErr[m.key(owner, repo)]; err != nil {
		return nil, err
	}
	return m.hooksByRepo[m.key(owner, repo)], nil
}

func (m *mockAdminClient) CreateWebhook(_ context.Context, owner, repo, pat string, h Webhook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, createWebhookCall{owner, repo, pat, h})
	return m.createErr[m.key(owner, repo)]
}

func (m *mockAdminClient) UpdateWebhook(_ context.Context, owner, repo string, hookID int64, pat string, h Webhook) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, updateWebhookCall{owner, repo, pat, hookID, h})
	return m.updateErr[m.key(owner, repo)]
}

func (m *mockAdminClient) SetActionsSecret(_ context.Context, owner, repo, name, plaintext, pat string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.secretCalls = append(m.secretCalls, setActionsSecretCall{owner, repo, name, plaintext, pat})
	return m.secretErr[m.key(owner, repo)]
}

// ── Helpers ────────────────────────────────────────────────────────────────

const testWebhookURL = "https://api.example.com/webhook"

func newReconciler(reg *targets.Registry, m *mockAdminClient) *Reconciler {
	return NewReconciler(reg, m, testWebhookURL)
}

func target(owner, repo, pat, secret, fwKey string) targets.TargetConfig {
	return targets.TargetConfig{
		RepoOwner:       owner,
		RepoName:        repo,
		GitHubPAT:       pat,
		WebhookSecret:   secret,
		FireworksAPIKey: fwKey,
	}
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestReconciler_EmptyRegistry_DoesNothing(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	m := newMockAdmin()

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.listCalls) != 0 || len(m.createCalls) != 0 || len(m.updateCalls) != 0 || len(m.secretCalls) != 0 {
		t.Errorf("expected no calls; got list=%d create=%d update=%d secret=%d",
			len(m.listCalls), len(m.createCalls), len(m.updateCalls), len(m.secretCalls))
	}
}

func TestReconciler_NoExistingWebhook_CreatesAndSetsSecret(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "demo", "ghp_admin", "whsec123", "fw_key"))
	m := newMockAdmin()

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.createCalls) != 1 {
		t.Fatalf("create calls = %d, want 1", len(m.createCalls))
	}
	created := m.createCalls[0]
	if created.Owner != "dx-tooling" || created.Repo != "demo" {
		t.Errorf("create owner/repo = %s/%s", created.Owner, created.Repo)
	}
	if created.Hook.URL != testWebhookURL {
		t.Errorf("create URL = %s, want %s", created.Hook.URL, testWebhookURL)
	}
	if created.Hook.Secret != "whsec123" {
		t.Errorf("create Secret = %s, want whsec123", created.Hook.Secret)
	}
	if !created.Hook.Active {
		t.Errorf("create Active = false, want true")
	}
	wantEvents := []string{"pull_request", "issues", "issue_comment"}
	if len(created.Hook.Events) != len(wantEvents) {
		t.Errorf("create Events = %v, want %v", created.Hook.Events, wantEvents)
	}

	if len(m.updateCalls) != 0 {
		t.Errorf("expected no update calls; got %d", len(m.updateCalls))
	}
	if len(m.secretCalls) != 1 {
		t.Fatalf("secret calls = %d, want 1", len(m.secretCalls))
	}
	if m.secretCalls[0].Plaintext != "fw_key" || m.secretCalls[0].Name != "FIREWORKS_API_KEY" {
		t.Errorf("secret call = %+v", m.secretCalls[0])
	}
}

func TestReconciler_ExistingMatchingWebhook_NoCreateNoUpdate(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "demo", "ghp_admin", "whsec123", "fw_key"))
	m := newMockAdmin()
	m.hooksByRepo["dx-tooling/demo"] = []Webhook{
		{ID: 99, URL: testWebhookURL, Active: true, Events: []string{"pull_request", "issues", "issue_comment"}},
	}

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.createCalls) != 0 {
		t.Errorf("create calls = %d, want 0", len(m.createCalls))
	}
	if len(m.updateCalls) != 0 {
		t.Errorf("update calls = %d, want 0", len(m.updateCalls))
	}
	if len(m.secretCalls) != 1 {
		t.Errorf("secret calls = %d, want 1", len(m.secretCalls))
	}
}

func TestReconciler_ExistingWebhook_DifferentEvents_TriggersUpdate(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "demo", "ghp_admin", "whsec123", "fw_key"))
	m := newMockAdmin()
	m.hooksByRepo["dx-tooling/demo"] = []Webhook{
		{ID: 99, URL: testWebhookURL, Active: true, Events: []string{"pull_request"}},
	}

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.updateCalls) != 1 {
		t.Fatalf("update calls = %d, want 1", len(m.updateCalls))
	}
	if m.updateCalls[0].HookID != 99 {
		t.Errorf("update hookID = %d, want 99", m.updateCalls[0].HookID)
	}
	if m.updateCalls[0].Hook.Secret != "whsec123" {
		t.Errorf("update Secret = %s, want whsec123 (every update re-asserts secret)", m.updateCalls[0].Hook.Secret)
	}
}

func TestReconciler_ExistingWebhook_Inactive_TriggersUpdate(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "demo", "ghp_admin", "whsec123", "fw_key"))
	m := newMockAdmin()
	m.hooksByRepo["dx-tooling/demo"] = []Webhook{
		{ID: 99, URL: testWebhookURL, Active: false, Events: []string{"pull_request", "issues", "issue_comment"}},
	}

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.updateCalls) != 1 {
		t.Fatalf("update calls = %d, want 1 (active flag drift)", len(m.updateCalls))
	}
}

func TestReconciler_EmptyFireworksKey_SkipsActionsSecret(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "demo", "ghp_admin", "whsec123", ""))
	m := newMockAdmin()

	newReconciler(reg, m).ReconcileAll(context.Background())

	if len(m.createCalls) != 1 {
		t.Errorf("create calls = %d, want 1 (webhook still ensured)", len(m.createCalls))
	}
	if len(m.secretCalls) != 0 {
		t.Errorf("secret calls = %d, want 0 (no Fireworks key to set)", len(m.secretCalls))
	}
}

func TestReconciler_ListFails_SkipsTargetButContinuesOthers(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "broken", "ghp_bad", "whsec1", "fw_1"))
	reg.Register(target("luminor-project", "good", "ghp_admin", "whsec2", "fw_2"))
	m := newMockAdmin()
	m.listErr["dx-tooling/broken"] = errors.New("403 forbidden")

	newReconciler(reg, m).ReconcileAll(context.Background())

	// "good" target gets create + secret. "broken" gets list (fails) and nothing else.
	if len(m.createCalls) != 1 || m.createCalls[0].Repo != "good" {
		t.Errorf("create calls = %v, want 1 for 'good'", m.createCalls)
	}
	if len(m.secretCalls) != 1 || m.secretCalls[0].Repo != "good" {
		t.Errorf("secret calls = %v, want 1 for 'good'", m.secretCalls)
	}
}

func TestReconciler_SecretFails_DoesNotBlockOtherTargets(t *testing.T) {
	reg := targets.NewRegistry("#productbuilding-")
	reg.Register(target("dx-tooling", "alpha", "ghp_admin", "whsec1", "fw_1"))
	reg.Register(target("dx-tooling", "beta", "ghp_admin", "whsec2", "fw_2"))
	m := newMockAdmin()
	m.secretErr["dx-tooling/alpha"] = errors.New("secret PUT failed")

	newReconciler(reg, m).ReconcileAll(context.Background())

	// Both targets should have webhook + attempted secret; no panic.
	if len(m.createCalls) != 2 {
		t.Errorf("create calls = %d, want 2", len(m.createCalls))
	}
	if len(m.secretCalls) != 2 {
		t.Errorf("secret calls = %d, want 2 (both attempted)", len(m.secretCalls))
	}
}
