package domain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr bool
	}{
		{"pending to building", StatusPending, StatusBuilding, false},
		{"building to deploying", StatusBuilding, StatusDeploying, false},
		{"building to failed", StatusBuilding, StatusFailed, false},
		{"deploying to ready", StatusDeploying, StatusReady, false},
		{"deploying to failed", StatusDeploying, StatusFailed, false},
		{"ready to pending (rebuild)", StatusReady, StatusPending, false},
		{"ready to deleted", StatusReady, StatusDeleted, false},
		{"failed to pending (retry)", StatusFailed, StatusPending, false},
		{"failed to deleted", StatusFailed, StatusDeleted, false},

		// Invalid transitions
		{"pending to ready", StatusPending, StatusReady, true},
		{"pending to deleted", StatusPending, StatusDeleted, true},
		{"building to ready", StatusBuilding, StatusReady, true},
		{"ready to building", StatusReady, StatusBuilding, true},
		{"deleted to anything", StatusDeleted, StatusPending, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTransition(%q, %q) error = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}

// --- SlackThreadChecker mock ---

type mockSlackThreadChecker struct {
	hasThread bool
}

func (m *mockSlackThreadChecker) HasThread(ctx context.Context, repoOwner, repoName string, prNumber int) bool {
	return m.hasThread
}

func TestWithSlackThreadChecker_SetsField(t *testing.T) {
	checker := &mockSlackThreadChecker{hasThread: true}
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, "", "", WithSlackThreadChecker(checker))

	if svc.slackThreadChecker == nil {
		t.Fatal("expected slackThreadChecker to be set")
	}
}

func TestWithSlackThreadChecker_NilByDefault(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, "", "")

	if svc.slackThreadChecker != nil {
		t.Fatal("expected slackThreadChecker to be nil by default")
	}
}

// =============================================================================
// Mock implementations for DeployPreview integration tests
// =============================================================================

// --- mockRepository ---

type mockRepository struct {
	mu       sync.Mutex
	previews map[string]Preview // key: "owner/repo#pr"
	calls    []string
}

func newMockRepository() *mockRepository {
	return &mockRepository{previews: make(map[string]Preview)}
}

func (m *mockRepository) key(owner, repo string, pr int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, pr)
}

func (m *mockRepository) Upsert(ctx context.Context, p Preview) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "Upsert")
	m.previews[m.key(p.RepoOwner, p.RepoName, p.PRNumber)] = p
	return nil
}

func (m *mockRepository) FindByRepoPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "FindByRepoPR")
	p, ok := m.previews[m.key(repoOwner, repoName, prNumber)]
	if !ok {
		return nil, nil
	}
	copy := p
	return &copy, nil
}

func (m *mockRepository) ListActive(ctx context.Context) ([]Preview, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "ListActive")
	var result []Preview
	for _, p := range m.previews {
		if p.Status != StatusDeleted {
			result = append(result, p)
		}
	}
	return result, nil
}

func (m *mockRepository) UpdateStatus(ctx context.Context, id string, status Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "UpdateStatus")
	for k, p := range m.previews {
		if p.ID == id {
			p.Status = status
			m.previews[k] = p
			return nil
		}
	}
	return nil
}

func (m *mockRepository) Update(ctx context.Context, p Preview) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, "Update")
	m.previews[m.key(p.RepoOwner, p.RepoName, p.PRNumber)] = p
	return nil
}

func (m *mockRepository) getPreview(owner, repo string, pr int) *Preview {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.previews[m.key(owner, repo, pr)]
	if !ok {
		return nil
	}
	return &p
}

// --- mockSourceDownloader ---

type downloadCall struct {
	Owner, Repo, SHA, PAT, DestDir string
}

type mockSourceDownloader struct {
	mu           sync.Mutex
	calls        []downloadCall
	contractYAML string
	err          error
}

func (m *mockSourceDownloader) DownloadSource(ctx context.Context, owner, repo, sha, pat, destDir string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, downloadCall{owner, repo, sha, pat, destDir})
	if m.err != nil {
		return "", m.err
	}
	// Write contract file so ParseContract can read it
	contractDir := filepath.Join(destDir, ".productbuilding", "preview")
	if err := os.MkdirAll(contractDir, 0755); err != nil {
		return "", fmt.Errorf("mock: mkdir contract dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(contractDir, "config.yml"), []byte(m.contractYAML), 0644); err != nil {
		return "", fmt.Errorf("mock: write contract: %w", err)
	}
	// Write placeholder compose file
	if err := os.WriteFile(filepath.Join(destDir, "docker-compose.yml"), []byte("version: '3'\nservices:\n  app:\n    image: nginx\n"), 0644); err != nil {
		return "", fmt.Errorf("mock: write compose: %w", err)
	}
	return destDir, nil
}

func defaultContractYAML() string {
	return `version: 1
compose:
  file: docker-compose.yml
  service: app
runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 30
`
}

func contractWithMigrations() string {
	return `version: 1
compose:
  file: docker-compose.yml
  service: app
runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 30
database:
  migrate_command: "php artisan migrate --force"
`
}

func contractWithUserNote() string {
	return `version: 1
compose:
  file: docker-compose.yml
  service: app
runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 30
user_facing_note: "Login with test@example.com"
`
}

func contractWithPostDeploy() string {
	return `version: 1
compose:
  file: docker-compose.yml
  service: app
runtime:
  internal_port: 8080
  healthcheck_path: /healthz
  startup_timeout_seconds: 30
post_deploy_commands:
  - command: "php artisan db:seed"
    description: "Seed database"
`
}

// --- mockComposeManager ---

type generateOverrideCall struct {
	WorkDir, ServiceName, RouterName, Host string
	Port                                   int
}

type composeUpCall struct {
	ProjectName, WorkDir string
	ComposeFiles         []string
	ExtraEnv             []string
}

type composeDownCall struct {
	ProjectName, WorkDir string
}

type composeExecCall struct {
	ProjectName, ServiceName, WorkDir string
	Command                           []string
}

type mockComposeManager struct {
	mu            sync.Mutex
	generateCalls []generateOverrideCall
	upCalls       []composeUpCall
	downCalls     []composeDownCall
	execCalls     []composeExecCall

	generateErr error
	upErr       error
	downErr     error
	execErr     error
}

func (m *mockComposeManager) GenerateOverride(workDir, serviceName, routerName, host string, port int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generateCalls = append(m.generateCalls, generateOverrideCall{workDir, serviceName, routerName, host, port})
	if m.generateErr != nil {
		return "", m.generateErr
	}
	// Write a minimal override file — the service takes filepath.Rel of the returned path
	overrideDir := filepath.Join(workDir, ".productbuilding", "preview")
	os.MkdirAll(overrideDir, 0755)
	overridePath := filepath.Join(overrideDir, "docker-compose.override.yml")
	os.WriteFile(overridePath, []byte("version: '3'\n"), 0644)
	return overridePath, nil
}

func (m *mockComposeManager) Up(ctx context.Context, projectName, workDir string, composeFiles []string, extraEnv []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upCalls = append(m.upCalls, composeUpCall{projectName, workDir, composeFiles, extraEnv})
	return m.upErr
}

func (m *mockComposeManager) Down(ctx context.Context, projectName, workDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.downCalls = append(m.downCalls, composeDownCall{projectName, workDir})
	return m.downErr
}

func (m *mockComposeManager) IsRunning(ctx context.Context, projectName string) (bool, error) {
	return false, nil
}

func (m *mockComposeManager) Exec(ctx context.Context, projectName, serviceName, workDir string, command []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, composeExecCall{projectName, serviceName, workDir, command})
	return m.execErr
}

func (m *mockComposeManager) Logs(ctx context.Context, projectName, serviceName string, tail int, follow bool, w io.Writer) error {
	return nil
}

func (m *mockComposeManager) LogsFromFile(ctx context.Context, projectName, serviceName, workDir, logPath string, tail int, follow bool, w io.Writer) error {
	return nil
}

// --- mockHealthChecker ---

type healthCheckCall struct {
	URL     string
	Timeout time.Duration
}

type mockHealthChecker struct {
	mu           sync.Mutex
	healthyCalls []healthCheckCall
	tlsCalls     []healthCheckCall
	healthyErr   error
	tlsErr       error
}

func (m *mockHealthChecker) WaitForHealthy(ctx context.Context, url string, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthyCalls = append(m.healthyCalls, healthCheckCall{URL: url, Timeout: timeout})
	return m.healthyErr
}

func (m *mockHealthChecker) WaitForTLS(ctx context.Context, url string, timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tlsCalls = append(m.tlsCalls, healthCheckCall{URL: url, Timeout: timeout})
	return m.tlsErr
}

// --- mockPRCommenter ---

type commentCreateCall struct {
	Owner, Repo string
	PRNumber    int
	Body, PAT   string
}

type commentUpdateCall struct {
	Owner, Repo string
	CommentID   int64
	Body, PAT   string
}

type commentDeleteCall struct {
	Owner, Repo string
	CommentID   int64
	PAT         string
}

type commentDeleteAllCall struct {
	Owner, Repo string
	PRNumber    int
	PAT         string
}

type mockPRCommenter struct {
	mu                sync.Mutex
	createCalls       []commentCreateCall
	updateCalls       []commentUpdateCall
	deleteCalls       []commentDeleteCall
	deleteAllBotCalls []commentDeleteAllCall

	createCommentID int64
	createErr       error
	updateErr       error
	deleteErr       error
	deleteAllErr    error
}

func (m *mockPRCommenter) CreateComment(ctx context.Context, owner, repo string, prNumber int, body, pat string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.createCalls = append(m.createCalls, commentCreateCall{owner, repo, prNumber, body, pat})
	return m.createCommentID, m.createErr
}

func (m *mockPRCommenter) UpdateComment(ctx context.Context, owner, repo string, commentID int64, body, pat string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, commentUpdateCall{owner, repo, commentID, body, pat})
	return m.updateErr
}

func (m *mockPRCommenter) DeleteComment(ctx context.Context, owner, repo string, commentID int64, pat string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls = append(m.deleteCalls, commentDeleteCall{owner, repo, commentID, pat})
	return m.deleteErr
}

func (m *mockPRCommenter) DeleteAllBotComments(ctx context.Context, owner, repo string, prNumber int, pat string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteAllBotCalls = append(m.deleteAllBotCalls, commentDeleteAllCall{owner, repo, prNumber, pat})
	return m.deleteAllErr
}

// --- mockSlackNotifier ---

type notifyCall struct {
	EventType         slackfacade.EventType
	RepoOwner         string
	RepoName          string
	PRNumber          int
	LinkedIssueNumber int
	Status            string
	UserNote          string
	CtxCanceled       bool // true if context was already canceled when Notify was called
}

type mockSlackNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
	ctxs  []context.Context // saved contexts for post-call inspection
	err   error
}

func (m *mockSlackNotifier) Notify(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, notifyCall{
		EventType:         event.Type,
		RepoOwner:         event.RepoOwner,
		RepoName:          event.RepoName,
		PRNumber:          event.IssueNumber,
		LinkedIssueNumber: event.LinkedIssueNumber,
		Status:            event.Status,
		UserNote:          event.UserNote,
		CtxCanceled:       ctx.Err() != nil,
	})
	m.ctxs = append(m.ctxs, ctx)
	return m.err
}

// =============================================================================
// Test helpers
// =============================================================================

type testDeps struct {
	svc       *Service
	repo      *mockRepository
	dl        *mockSourceDownloader
	compose   *mockComposeManager
	health    *mockHealthChecker
	commenter *mockPRCommenter
	notifier  *mockSlackNotifier
}

func setupTestService(t *testing.T, opts ...ServiceOption) testDeps {
	t.Helper()
	repo := newMockRepository()
	dl := &mockSourceDownloader{contractYAML: defaultContractYAML()}
	compose := &mockComposeManager{}
	health := &mockHealthChecker{}
	commenter := &mockPRCommenter{createCommentID: 100}
	notifier := &mockSlackNotifier{}

	registry := targets.NewRegistry("productbuilding-")
	registry.Register(targets.TargetConfig{
		RepoOwner:     "example-org",
		RepoName:      "my-app",
		GitHubPAT:     "ghp_test",
		SlackChannel:  "#productbuilding-my-app",
		SlackBotToken: "xoxb-test",
	})

	workDir := t.TempDir()

	allOpts := append([]ServiceOption{}, opts...)
	svc := NewService(repo, dl, compose, health, commenter, notifier, registry, "preview.example.com", workDir, allOpts...)

	return testDeps{svc, repo, dl, compose, health, commenter, notifier}
}

func testDeployRequest() DeployRequest {
	return DeployRequest{
		RepoOwner: "example-org",
		RepoName:  "my-app",
		PRNumber:  42,
		Branch:    "feature/test",
		HeadSHA:   "abc123def456789012345678901234567890abcd",
	}
}

// =============================================================================
// Integration tests for DeployPreview
// =============================================================================

func TestDeployPreview_HappyPath(t *testing.T) {
	d := setupTestService(t)
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	// Source downloaded once
	d.dl.mu.Lock()
	if len(d.dl.calls) != 1 {
		t.Fatalf("expected 1 download call, got %d", len(d.dl.calls))
	}
	dlCall := d.dl.calls[0]
	d.dl.mu.Unlock()

	if dlCall.Owner != "example-org" || dlCall.Repo != "my-app" || dlCall.SHA != req.HeadSHA {
		t.Errorf("download called with wrong args: %+v", dlCall)
	}
	if dlCall.PAT != "ghp_test" {
		t.Errorf("download PAT = %q, want %q", dlCall.PAT, "ghp_test")
	}

	// Compose override generated
	d.compose.mu.Lock()
	if len(d.compose.generateCalls) != 1 {
		t.Fatalf("expected 1 generate call, got %d", len(d.compose.generateCalls))
	}
	gen := d.compose.generateCalls[0]
	if gen.ServiceName != "app" {
		t.Errorf("service name = %q, want %q", gen.ServiceName, "app")
	}
	if gen.Host != "my-app-pr-42.preview.example.com" {
		t.Errorf("host = %q, want %q", gen.Host, "my-app-pr-42.preview.example.com")
	}
	if gen.Port != 8080 {
		t.Errorf("port = %d, want %d", gen.Port, 8080)
	}

	// Compose up called
	if len(d.compose.upCalls) != 1 {
		t.Fatalf("expected 1 compose up call, got %d", len(d.compose.upCalls))
	}
	up := d.compose.upCalls[0]
	if up.ProjectName != "my-app_pr_42" {
		t.Errorf("project name = %q, want %q", up.ProjectName, "my-app_pr_42")
	}
	// Check GITHUB_TOKEN in env
	foundToken := false
	for _, env := range up.ExtraEnv {
		if env == "GITHUB_TOKEN=ghp_test" {
			foundToken = true
		}
	}
	if !foundToken {
		t.Errorf("expected GITHUB_TOKEN=ghp_test in env, got %v", up.ExtraEnv)
	}
	d.compose.mu.Unlock()

	// Health checks called
	d.health.mu.Lock()
	if len(d.health.healthyCalls) != 1 {
		t.Errorf("expected 1 health check call, got %d", len(d.health.healthyCalls))
	}
	if len(d.health.tlsCalls) != 1 {
		t.Errorf("expected 1 TLS check call, got %d", len(d.health.tlsCalls))
	}
	d.health.mu.Unlock()

	// Bot comments deleted and ack posted
	d.commenter.mu.Lock()
	if len(d.commenter.deleteAllBotCalls) != 1 {
		t.Errorf("expected 1 deleteAllBotComments call, got %d", len(d.commenter.deleteAllBotCalls))
	}
	if len(d.commenter.createCalls) != 1 {
		t.Errorf("expected 1 createComment call, got %d", len(d.commenter.createCalls))
	}
	if len(d.commenter.updateCalls) == 0 {
		t.Error("expected at least 1 updateComment call for progress")
	}
	d.commenter.mu.Unlock()

	// Slack notified with ready
	d.notifier.mu.Lock()
	if len(d.notifier.calls) != 1 {
		t.Fatalf("expected 1 slack notification, got %d", len(d.notifier.calls))
	}
	if d.notifier.calls[0].EventType != slackfacade.EventPRReady {
		t.Errorf("event type = %q, want %q", d.notifier.calls[0].EventType, slackfacade.EventPRReady)
	}
	d.notifier.mu.Unlock()

	// Final preview state
	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusReady {
		t.Errorf("status = %q, want %q", p.Status, StatusReady)
	}
	if p.HeadSHA != req.HeadSHA {
		t.Errorf("HeadSHA = %q, want %q", p.HeadSHA, req.HeadSHA)
	}
	if p.LastSuccessfulSHA != req.HeadSHA {
		t.Errorf("LastSuccessfulSHA = %q, want %q", p.LastSuccessfulSHA, req.HeadSHA)
	}
	if p.ErrorStage != "" || p.ErrorMessage != "" {
		t.Errorf("expected empty error fields, got stage=%q msg=%q", p.ErrorStage, p.ErrorMessage)
	}
}

func TestDeployPreview_DownloadFails(t *testing.T) {
	d := setupTestService(t)
	d.dl.err = fmt.Errorf("network timeout")
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	// Preview should be failed
	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusFailed {
		t.Errorf("status = %q, want %q", p.Status, StatusFailed)
	}
	if !strings.Contains(p.ErrorStage, "download") {
		t.Errorf("ErrorStage = %q, want to contain 'download'", p.ErrorStage)
	}

	// Compose should never be called
	d.compose.mu.Lock()
	if len(d.compose.upCalls) != 0 {
		t.Errorf("expected 0 compose up calls, got %d", len(d.compose.upCalls))
	}
	d.compose.mu.Unlock()

	// Slack notified with failed
	d.notifier.mu.Lock()
	foundFailed := false
	for _, call := range d.notifier.calls {
		if call.EventType == slackfacade.EventPRFailed {
			foundFailed = true
		}
	}
	d.notifier.mu.Unlock()
	if !foundFailed {
		t.Error("expected Slack notification with EventPRFailed")
	}
}

func TestDeployPreview_ComposeUpFails(t *testing.T) {
	d := setupTestService(t)
	d.compose.upErr = fmt.Errorf("build failed")
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusFailed {
		t.Errorf("status = %q, want %q", p.Status, StatusFailed)
	}
	if !strings.Contains(p.ErrorStage, "compose_up") {
		t.Errorf("ErrorStage = %q, want to contain 'compose_up'", p.ErrorStage)
	}

	// Health check should never be called
	d.health.mu.Lock()
	if len(d.health.healthyCalls) != 0 {
		t.Errorf("expected 0 health check calls, got %d", len(d.health.healthyCalls))
	}
	d.health.mu.Unlock()
}

func TestDeployPreview_HealthCheckFails(t *testing.T) {
	d := setupTestService(t)
	d.health.healthyErr = fmt.Errorf("timed out")
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusFailed {
		t.Errorf("status = %q, want %q", p.Status, StatusFailed)
	}
	if !strings.Contains(p.ErrorStage, "healthcheck") {
		t.Errorf("ErrorStage = %q, want to contain 'healthcheck'", p.ErrorStage)
	}

	// TLS check should never be called
	d.health.mu.Lock()
	if len(d.health.tlsCalls) != 0 {
		t.Errorf("expected 0 TLS check calls, got %d", len(d.health.tlsCalls))
	}
	d.health.mu.Unlock()
}

func TestDeployPreview_TLSFails(t *testing.T) {
	d := setupTestService(t)
	d.health.tlsErr = fmt.Errorf("certificate not valid")
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusFailed {
		t.Errorf("status = %q, want %q", p.Status, StatusFailed)
	}
	if !strings.Contains(p.ErrorStage, "tls") {
		t.Errorf("ErrorStage = %q, want to contain 'tls'", p.ErrorStage)
	}
}

func TestDeployPreview_ExistingPreview_PreservesIdentity(t *testing.T) {
	d := setupTestService(t)

	// Pre-seed an existing preview
	existing := NewPreview("example-org", "my-app", 42, "feature/old", "oldsha123456789012345678901234567890old", "preview.example.com")
	existing.Status = StatusReady
	existing.LastSuccessfulSHA = "oldsha123456789012345678901234567890old"
	d.repo.Upsert(context.Background(), existing)

	originalID := existing.ID
	originalComposeProject := existing.ComposeProject
	originalPreviewURL := existing.PreviewURL
	originalCreatedAt := existing.CreatedAt

	req := testDeployRequest()
	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.ID != originalID {
		t.Errorf("ID changed: %q → %q", originalID, p.ID)
	}
	if p.ComposeProject != originalComposeProject {
		t.Errorf("ComposeProject changed: %q → %q", originalComposeProject, p.ComposeProject)
	}
	if p.PreviewURL != originalPreviewURL {
		t.Errorf("PreviewURL changed: %q → %q", originalPreviewURL, p.PreviewURL)
	}
	if !p.CreatedAt.Equal(originalCreatedAt) {
		t.Errorf("CreatedAt changed: %v → %v", originalCreatedAt, p.CreatedAt)
	}
	if p.HeadSHA != req.HeadSHA {
		t.Errorf("HeadSHA = %q, want %q", p.HeadSHA, req.HeadSHA)
	}
	if p.Status != StatusReady {
		t.Errorf("status = %q, want %q", p.Status, StatusReady)
	}
}

func TestDeployPreview_SlackTracking_StillUsesFullChecklist(t *testing.T) {
	// GitHub comments must always use the full progress checklist, even when
	// Slack is tracking the PR. GitHub is the source of truth for developers
	// while Slack serves the product owner — neither should be compromised.
	d := setupTestService(t, WithSlackThreadChecker(&mockSlackThreadChecker{hasThread: true}))
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.commenter.mu.Lock()
	defer d.commenter.mu.Unlock()

	// Ack comment should be the full "Preview deploying" format, not the minimal "tracked in Slack"
	if len(d.commenter.createCalls) != 1 {
		t.Fatalf("expected 1 create call, got %d", len(d.commenter.createCalls))
	}
	ackBody := d.commenter.createCalls[0].Body
	if strings.Contains(ackBody, "tracked in Slack") {
		t.Errorf("ack body should NOT use minimal Slack-tracking format, got: %s", ackBody)
	}
	if !strings.Contains(ackBody, "Preview deploying") {
		t.Errorf("ack body should use full progress format, got: %s", ackBody)
	}

	// Should have multiple update calls (full progress checklist updates)
	if len(d.commenter.updateCalls) < 2 {
		t.Errorf("expected multiple progress updates (full checklist), got %d", len(d.commenter.updateCalls))
	}

	// Final comment should include the checklist with checkmarks
	if len(d.commenter.updateCalls) > 0 {
		finalBody := d.commenter.updateCalls[len(d.commenter.updateCalls)-1].Body
		if !strings.Contains(finalBody, "- [x]") {
			t.Errorf("final comment should have completed checklist items, got: %s", finalBody)
		}
		if !strings.Contains(finalBody, "my-app-pr-42.preview.example.com") {
			t.Errorf("final comment should contain preview URL, got: %s", finalBody)
		}
	}
}

func TestDeployPreview_WithDatabaseMigrations(t *testing.T) {
	d := setupTestService(t)
	d.dl.contractYAML = contractWithMigrations()
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.compose.mu.Lock()
	defer d.compose.mu.Unlock()

	// Should have at least one exec call for migrations
	foundMigration := false
	for _, call := range d.compose.execCalls {
		for _, arg := range call.Command {
			if strings.Contains(arg, "php artisan migrate") {
				foundMigration = true
			}
		}
	}
	if !foundMigration {
		t.Errorf("expected migration exec call, got: %+v", d.compose.execCalls)
	}

	// Preview should be ready (migrations succeeded)
	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusReady {
		t.Errorf("status = %q, want %q", p.Status, StatusReady)
	}
}

func TestDeployPreview_WithPostDeployCommands(t *testing.T) {
	d := setupTestService(t)
	d.dl.contractYAML = contractWithPostDeploy()
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.compose.mu.Lock()
	defer d.compose.mu.Unlock()

	foundPostDeploy := false
	for _, call := range d.compose.execCalls {
		for _, arg := range call.Command {
			if strings.Contains(arg, "php artisan db:seed") {
				foundPostDeploy = true
			}
		}
	}
	if !foundPostDeploy {
		t.Errorf("expected post-deploy exec call, got: %+v", d.compose.execCalls)
	}

	p := d.repo.getPreview("example-org", "my-app", 42)
	if p == nil {
		t.Fatal("expected preview to exist")
	}
	if p.Status != StatusReady {
		t.Errorf("status = %q, want %q", p.Status, StatusReady)
	}
}

func TestDeployPreview_ContextCancellation(t *testing.T) {
	d := setupTestService(t)

	// Make downloader check context — simulate cancellation during download
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately
	d.dl.err = ctx.Err()

	req := testDeployRequest()
	d.svc.DeployPreview(ctx, req, "ghp_test")

	// Compose should never be called
	d.compose.mu.Lock()
	if len(d.compose.upCalls) != 0 {
		t.Errorf("expected 0 compose up calls after cancellation, got %d", len(d.compose.upCalls))
	}
	d.compose.mu.Unlock()
}

func TestDeployPreview_ReadyNotification_IncludesUserNote(t *testing.T) {
	d := setupTestService(t)
	d.dl.contractYAML = contractWithUserNote()
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.notifier.mu.Lock()
	defer d.notifier.mu.Unlock()

	var readyCall *notifyCall
	for i, call := range d.notifier.calls {
		if call.EventType == slackfacade.EventPRReady {
			readyCall = &d.notifier.calls[i]
			break
		}
	}

	if readyCall == nil {
		t.Fatal("Expected EventPRReady notification")
	}
	if readyCall.UserNote != "Login with test@example.com" {
		t.Errorf("Expected UserNote %q, got %q", "Login with test@example.com", readyCall.UserNote)
	}
}

func TestDeployPreview_FailedNotification_NoUserNote(t *testing.T) {
	d := setupTestService(t)
	d.dl.contractYAML = contractWithUserNote()
	d.health.healthyErr = fmt.Errorf("timed out")
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.notifier.mu.Lock()
	defer d.notifier.mu.Unlock()

	var failedCall *notifyCall
	for i, call := range d.notifier.calls {
		if call.EventType == slackfacade.EventPRFailed {
			failedCall = &d.notifier.calls[i]
			break
		}
	}

	if failedCall == nil {
		t.Fatal("Expected EventPRFailed notification")
	}
	if failedCall.UserNote != "" {
		t.Errorf("Expected empty UserNote on failure, got %q", failedCall.UserNote)
	}
}

func TestDeployPreview_PassesLinkedIssueNumber(t *testing.T) {
	d := setupTestService(t)
	req := testDeployRequest()
	req.LinkedIssueNumber = 101

	d.svc.DeployPreview(context.Background(), req, "ghp_test")

	d.notifier.mu.Lock()
	defer d.notifier.mu.Unlock()

	var readyCall *notifyCall
	for i, call := range d.notifier.calls {
		if call.EventType == slackfacade.EventPRReady {
			readyCall = &d.notifier.calls[i]
			break
		}
	}

	if readyCall == nil {
		t.Fatal("Expected EventPRReady notification")
	}
	if readyCall.LinkedIssueNumber != 101 {
		t.Errorf("Expected LinkedIssueNumber 101, got %d", readyCall.LinkedIssueNumber)
	}
}

func TestDeployPreview_NotificationContextNotCanceled(t *testing.T) {
	// The deploy method uses a cancellable context (defer cancel()) but
	// the notifier debounces and runs DB queries after the function returns.
	// The contexts passed to Notify must still be valid after DeployPreview exits.
	d := setupTestService(t)
	req := testDeployRequest()

	d.svc.DeployPreview(context.Background(), req, "ghp_test")
	// At this point, DeployPreview has returned and defer cancel() has fired.

	d.notifier.mu.Lock()
	defer d.notifier.mu.Unlock()

	if len(d.notifier.ctxs) == 0 {
		t.Fatal("Expected at least one notification, got none")
	}

	// Check contexts AFTER DeployPreview returned — simulates the debounce delay.
	for i, ctx := range d.notifier.ctxs {
		if ctx.Err() != nil {
			t.Errorf("Notification %d (%s) context is canceled after deploy returned: %v — "+
				"debounced DB queries will fail with 'context canceled'",
				i, d.notifier.calls[i].EventType, ctx.Err())
		}
	}
}
