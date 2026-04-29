package domain

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// ── Mock PullRequestStateChecker ───────────────────────────────────────────

type prStateCall struct {
	Owner, Repo, PAT string
	Number           int
}

type mockPRStateChecker struct {
	mu sync.Mutex

	calls []prStateCall
	// open keyed by "owner/repo#pr"; absent → defaults to "open" so existing
	// tests that don't set anything see PRs as open.
	open map[string]bool
	// errors keyed by the same key; if set, the call returns this error.
	errs map[string]error
}

func newMockPRStateChecker() *mockPRStateChecker {
	return &mockPRStateChecker{
		open: make(map[string]bool),
		errs: make(map[string]error),
	}
}

func (m *mockPRStateChecker) IsPullRequestOpen(_ context.Context, owner, repo string, prNumber int, pat string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, prStateCall{owner, repo, pat, prNumber})
	key := keyOf(owner, repo, prNumber)
	if err, ok := m.errs[key]; ok {
		return false, err
	}
	if v, set := m.open[key]; set {
		return v, nil
	}
	return true, nil
}

func keyOf(owner, repo string, pr int) string {
	return owner + "/" + repo + "#" + itoa(pr)
}

func itoa(n int) string {
	// tiny inline to avoid importing strconv just here
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ── Helpers ────────────────────────────────────────────────────────────────

// reconcileSetup wires a Service whose registry contains "example-org/my-app"
// (matching setupTestService) plus a mockPRStateChecker. Returns the deps so
// tests can seed the repo + assert side effects.
func reconcileSetup(t *testing.T) (testDeps, *mockPRStateChecker) {
	t.Helper()
	prc := newMockPRStateChecker()
	d := setupTestService(t, WithPRStateChecker(prc))
	return d, prc
}

func seedPreview(t *testing.T, repo *mockRepository, owner, repoName string, pr int, status Status, headSHA string) Preview {
	t.Helper()
	p := NewPreview(owner, repoName, pr, "feature/x", headSHA, "preview.example.com")
	p.Status = status
	if err := repo.Upsert(context.Background(), p); err != nil {
		t.Fatalf("seed Upsert: %v", err)
	}
	return p
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestReconcileActive_NoPreviews_DoesNothing(t *testing.T) {
	d, prc := reconcileSetup(t)

	d.svc.ReconcileActive(context.Background())

	if len(prc.calls) != 0 {
		t.Errorf("expected no PR-state calls; got %d", len(prc.calls))
	}
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no download calls; got %d", len(d.dl.calls))
	}
}

func TestReconcileActive_OrphanSweep_MarksInFlightRowsFailed(t *testing.T) {
	d, _ := reconcileSetup(t)

	pending := seedPreview(t, d.repo, "example-org", "my-app", 1, StatusPending, "abcdef1234567890")
	building := seedPreview(t, d.repo, "example-org", "my-app", 2, StatusBuilding, "abcdef1234567890")
	deploying := seedPreview(t, d.repo, "example-org", "my-app", 3, StatusDeploying, "abcdef1234567890")
	// A failed row should NOT get re-marked
	failed := seedPreview(t, d.repo, "example-org", "my-app", 4, StatusFailed, "abcdef1234567890")

	d.svc.ReconcileActive(context.Background())

	// All three orphans should now be Failed
	for _, prev := range []Preview{pending, building, deploying} {
		got := d.repo.getPreview(prev.RepoOwner, prev.RepoName, prev.PRNumber)
		if got == nil {
			t.Fatalf("missing row for PR %d", prev.PRNumber)
		}
		if got.Status != StatusFailed {
			t.Errorf("PR %d status = %s, want %s", prev.PRNumber, got.Status, StatusFailed)
		}
		if got.ErrorStage != "startup-reconcile" {
			t.Errorf("PR %d ErrorStage = %q, want startup-reconcile", prev.PRNumber, got.ErrorStage)
		}
		if got.ErrorMessage == "" {
			t.Errorf("PR %d ErrorMessage empty, want a message", prev.PRNumber)
		}
	}
	// Failed row stays untouched (its ErrorMessage should not be the orphan-sweep one)
	stillFailed := d.repo.getPreview(failed.RepoOwner, failed.RepoName, failed.PRNumber)
	if stillFailed.ErrorMessage == "interrupted by orchestrator restart" {
		t.Errorf("pre-existing failed row was re-marked: %+v", stillFailed)
	}
}

func TestReconcileActive_ReadyRunning_NoOp(t *testing.T) {
	d, prc := reconcileSetup(t)

	seedPreview(t, d.repo, "example-org", "my-app", 5, StatusReady, "abcdef1234567890")
	d.compose.mu.Lock()
	d.compose.runningProjects = map[string]bool{"my-app_pr_5": true}
	d.compose.mu.Unlock()

	d.svc.ReconcileActive(context.Background())

	// IsRunning was the only probe needed; PR-state check should NOT have happened
	if len(prc.calls) != 0 {
		t.Errorf("expected no PR-state probe when container is running; got %d calls", len(prc.calls))
	}
	// No redeploy
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no download (no redeploy) when container is running; got %d", len(d.dl.calls))
	}
}

func TestReconcileActive_ReadyMissing_OpenPR_TriggersRedeploy(t *testing.T) {
	d, prc := reconcileSetup(t)
	seedPreview(t, d.repo, "example-org", "my-app", 6, StatusReady, "abcdef1234567890abcdef")
	// IsRunning defaults to false in the mock (running=nil)

	d.svc.ReconcileActive(context.Background())

	if len(prc.calls) != 1 {
		t.Fatalf("expected 1 PR-state call; got %d", len(prc.calls))
	}
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 1 {
		t.Fatalf("expected 1 download call (redeploy); got %d", len(d.dl.calls))
	}
	if d.dl.calls[0].SHA != "abcdef1234567890abcdef" {
		t.Errorf("redeploy SHA = %q, want abcdef1234567890abcdef", d.dl.calls[0].SHA)
	}
}

func TestReconcileActive_ReadyMissing_ClosedPR_MarksDeleted(t *testing.T) {
	d, prc := reconcileSetup(t)
	seeded := seedPreview(t, d.repo, "example-org", "my-app", 7, StatusReady, "abcdef1234567890abcdef")
	prc.open[keyOf("example-org", "my-app", 7)] = false

	d.svc.ReconcileActive(context.Background())

	got := d.repo.getPreview(seeded.RepoOwner, seeded.RepoName, seeded.PRNumber)
	if got == nil || got.Status != StatusDeleted {
		t.Errorf("expected status %s for closed PR; got %+v", StatusDeleted, got)
	}
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no redeploy for closed PR; got %d download calls", len(d.dl.calls))
	}
}

func TestReconcileActive_PRStateProbeError_SkipsTarget(t *testing.T) {
	d, prc := reconcileSetup(t)
	seedPreview(t, d.repo, "example-org", "my-app", 8, StatusReady, "abcdef1234567890abcdef")
	prc.errs[keyOf("example-org", "my-app", 8)] = errors.New("network blip")

	d.svc.ReconcileActive(context.Background())

	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no redeploy when PR state probe fails; got %d", len(d.dl.calls))
	}
}

func TestReconcileActive_TargetNotInRegistry_SkipsRow(t *testing.T) {
	d, prc := reconcileSetup(t)
	// Seed with a repo NOT in the test registry
	seedPreview(t, d.repo, "stranger-org", "unknown", 9, StatusReady, "abcdef1234567890abcdef")

	d.svc.ReconcileActive(context.Background())

	if len(prc.calls) != 0 {
		t.Errorf("expected no PR-state probe for missing target; got %d", len(prc.calls))
	}
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no redeploy for missing target; got %d", len(d.dl.calls))
	}
}

func TestReconcileActive_EmptyHeadSHA_SkipsRow(t *testing.T) {
	d, prc := reconcileSetup(t)
	seedPreview(t, d.repo, "example-org", "my-app", 10, StatusReady, "")

	d.svc.ReconcileActive(context.Background())

	if len(prc.calls) != 0 {
		t.Errorf("expected no PR-state probe for empty SHA; got %d", len(prc.calls))
	}
	d.dl.mu.Lock()
	defer d.dl.mu.Unlock()
	if len(d.dl.calls) != 0 {
		t.Errorf("expected no redeploy for empty SHA; got %d", len(d.dl.calls))
	}
}
