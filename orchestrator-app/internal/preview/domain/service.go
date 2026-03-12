package domain

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Deploy step indices for the progress checklist.
const (
	stepDownload   = 0
	stepContainers = 1
	stepMigrations = 2
	stepHealth     = 3
	stepTLS        = 4
	numSteps       = 5
)

var stepLabels = [numSteps]string{
	"Download source",
	"Build and start containers",
	"Run database migrations",
	"Health check",
	"TLS certificate",
}

type Service struct {
	repo          Repository
	downloader    SourceDownloader
	compose       ComposeManager
	healthChecker HealthChecker
	commenter     PRCommenter
	previewDomain string
	workspaceDir  string

	// Per-PR mutex to prevent concurrent operations on the same preview.
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

func NewService(
	repo Repository,
	downloader SourceDownloader,
	compose ComposeManager,
	healthChecker HealthChecker,
	commenter PRCommenter,
	previewDomain string,
	workspaceDir string,
) *Service {
	return &Service{
		repo:          repo,
		downloader:    downloader,
		compose:       compose,
		healthChecker: healthChecker,
		commenter:     commenter,
		previewDomain: previewDomain,
		workspaceDir:  workspaceDir,
		locks:         make(map[string]*sync.Mutex),
	}
}

func (s *Service) getLock(key string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if _, ok := s.locks[key]; !ok {
		s.locks[key] = &sync.Mutex{}
	}
	return s.locks[key]
}

func (s *Service) ListPreviews(ctx context.Context) ([]Preview, error) {
	return s.repo.ListActive(ctx)
}

func (s *Service) GetPreview(ctx context.Context, repoOwner, repoName string, prNumber int) (*Preview, error) {
	return s.repo.FindByRepoPR(ctx, repoOwner, repoName, prNumber)
}

// GetPreviewLogs streams logs from a preview's app container.
func (s *Service) GetPreviewLogs(ctx context.Context, repoOwner, repoName string, prNumber int, tail int, follow bool, w io.Writer) error {
	preview, err := s.repo.FindByRepoPR(ctx, repoOwner, repoName, prNumber)
	if err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)

	// Parse the contract to get the service name
	contract, err := ParseContract(workDir)
	if err != nil {
		return fmt.Errorf("failed to parse contract: %w", err)
	}

	return s.compose.Logs(ctx, preview.ComposeProject, contract.Compose.Service, tail, follow, w)
}

// DeployPreview handles the full lifecycle: download → build → deploy → healthcheck.
// Runs synchronously; callers should invoke in a goroutine for async processing.
func (s *Service) DeployPreview(ctx context.Context, req DeployRequest, pat string) {
	log := slog.With("repo", req.RepoOwner+"/"+req.RepoName, "pr", req.PRNumber, "sha", req.HeadSHA[:8])

	meta := commentMeta{
		Owner:        req.RepoOwner,
		Repo:         req.RepoName,
		SHA:          req.HeadSHA,
		Branch:       req.Branch,
		PreviewURL:   fmt.Sprintf("https://%s-pr-%d.%s", req.RepoName, req.PRNumber, s.previewDomain),
		AnimationURL: "https://raw.githubusercontent.com/luminor-project/assets/refs/heads/main/productbuilding/crane-building-animation-128x128.gif",
	}

	// 1. Delete previous bot comment and post new acknowledgment (before acquiring mutex)
	existing, _ := s.repo.FindByRepoPR(ctx, req.RepoOwner, req.RepoName, req.PRNumber)
	if existing != nil && existing.GithubCommentID > 0 {
		_ = s.commenter.DeleteComment(ctx, req.RepoOwner, req.RepoName, existing.GithubCommentID, pat)
	}

	ackBody := progressComment("Preview deploying", meta, 0, "Queued, waiting to start...")
	commentID, err := s.commenter.CreateComment(ctx, req.RepoOwner, req.RepoName, req.PRNumber, ackBody, pat)
	if err != nil {
		log.Warn("failed to post ack comment", "error", err)
	}

	lockKey := fmt.Sprintf("%s/%s#%d", req.RepoOwner, req.RepoName, req.PRNumber)
	mu := s.getLock(lockKey)
	mu.Lock()
	defer mu.Unlock()

	log.Info("starting preview deployment")

	// 2. Upsert preview record (pending)
	preview := NewPreview(req.RepoOwner, req.RepoName, req.PRNumber, req.Branch, req.HeadSHA, s.previewDomain)

	if existing != nil {
		preview.ID = existing.ID
		preview.ComposeProject = existing.ComposeProject
		preview.PreviewURL = existing.PreviewURL
		preview.CreatedAt = existing.CreatedAt
	}

	// Store the new comment ID on the preview
	preview.GithubCommentID = commentID

	if err := s.repo.Upsert(ctx, preview); err != nil {
		log.Error("failed to upsert preview", "error", err)
		return
	}

	// 3. Download source (status: building)
	s.setStatus(ctx, &preview, StatusBuilding, log)
	s.updateComment(ctx, &preview,
		progressComment("Preview deploying", meta, 0, "Downloading source..."),
		pat, log)

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		s.failPreview(ctx, &preview, stepDownload, "workspace", fmt.Sprintf("create workspace: %v", err), pat, log)
		return
	}

	if _, err := s.downloader.DownloadSource(ctx, req.RepoOwner, req.RepoName, req.HeadSHA, pat, workDir); err != nil {
		s.failPreview(ctx, &preview, stepDownload, "download", fmt.Sprintf("download source: %v", err), pat, log)
		return
	}

	// 4. Parse preview contract
	contract, err := ParseContract(workDir)
	if err != nil {
		s.failPreview(ctx, &preview, stepDownload+1, "contract", fmt.Sprintf("parse contract: %v", err), pat, log)
		return
	}

	// 5. Generate compose override with Traefik labels
	routerName := fmt.Sprintf("%s-pr-%d", req.RepoName, req.PRNumber)
	host := fmt.Sprintf("%s-pr-%d.%s", req.RepoName, req.PRNumber, s.previewDomain)

	composeFile := contract.Compose.File
	overridePath, err := s.compose.GenerateOverride(workDir, contract.Compose.Service, routerName, host, contract.Runtime.InternalPort)
	if err != nil {
		s.failPreview(ctx, &preview, stepDownload+1, "override", fmt.Sprintf("generate override: %v", err), pat, log)
		return
	}
	overrideRel, _ := filepath.Rel(workDir, overridePath)

	// 6. Docker compose up (status: deploying)
	s.setStatus(ctx, &preview, StatusDeploying, log)
	s.updateComment(ctx, &preview,
		progressComment("Preview deploying", meta, stepDownload+1, "Building and starting containers..."),
		pat, log)

	if err := s.compose.Up(ctx, preview.ComposeProject, workDir, []string{composeFile, overrideRel}); err != nil {
		s.failPreview(ctx, &preview, stepContainers, "compose_up", fmt.Sprintf("compose up: %v", err), pat, log)
		return
	}

	// 7. Run database migrations (if configured)
	if contract.Database.MigrateCommand != "" {
		s.updateComment(ctx, &preview,
			progressComment("Preview deploying", meta, stepContainers+1, "Running database migrations..."),
			pat, log)

		migrateCmd := []string{"sh", "-c", contract.Database.MigrateCommand}
		if err := s.compose.Exec(ctx, preview.ComposeProject, contract.Compose.Service, workDir, migrateCmd); err != nil {
			s.failPreview(ctx, &preview, stepMigrations, "migrations", fmt.Sprintf("database migrations: %v", err), pat, log)
			return
		}
	}

	// 8. Health check
	s.updateComment(ctx, &preview,
		progressComment("Preview deploying", meta, stepMigrations+1, "Running health check..."),
		pat, log)

	containerName := fmt.Sprintf("%s-%s-1", preview.ComposeProject, contract.Compose.Service)
	healthURL := fmt.Sprintf("http://%s:%d%s", containerName, contract.Runtime.InternalPort, contract.Runtime.HealthcheckPath)
	timeout := time.Duration(contract.Runtime.StartupTimeout) * time.Second

	if err := s.healthChecker.WaitForHealthy(ctx, healthURL, timeout); err != nil {
		s.failPreview(ctx, &preview, stepHealth, "healthcheck", fmt.Sprintf("health check: %v", err), pat, log)
		return
	}

	// 8. TLS certificate readiness (wait for Traefik to provision via Let's Encrypt)
	s.updateComment(ctx, &preview,
		progressComment("Preview deploying", meta, stepHealth+1, "Waiting for TLS certificate..."),
		pat, log)

	tlsTimeout := 120 * time.Second
	if err := s.healthChecker.WaitForTLS(ctx, meta.PreviewURL, tlsTimeout); err != nil {
		s.failPreview(ctx, &preview, stepTLS, "tls", fmt.Sprintf("TLS readiness: %v", err), pat, log)
		return
	}

	// 9. Ready!
	preview.Status = StatusReady
	preview.LastSuccessfulSHA = req.HeadSHA
	preview.ErrorStage = ""
	preview.ErrorMessage = ""
	_ = s.repo.Update(ctx, preview)

	s.updateComment(ctx, &preview,
		progressComment("Preview ready", meta, numSteps, fmt.Sprintf("**[%s](%s)**", meta.PreviewURL, meta.PreviewURL)),
		pat, log)

	log.Info("preview ready", "url", preview.PreviewURL)
}

// DeletePreview tears down the compose project, cleans up workspace, and marks deleted.
func (s *Service) DeletePreview(ctx context.Context, req DeployRequest, pat string) {
	lockKey := fmt.Sprintf("%s/%s#%d", req.RepoOwner, req.RepoName, req.PRNumber)
	mu := s.getLock(lockKey)
	mu.Lock()
	defer mu.Unlock()

	log := slog.With("repo", req.RepoOwner+"/"+req.RepoName, "pr", req.PRNumber)
	log.Info("deleting preview")

	preview, err := s.repo.FindByRepoPR(ctx, req.RepoOwner, req.RepoName, req.PRNumber)
	if err != nil {
		log.Info("no preview found for deletion")
		return
	}

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)
	if err := s.compose.Down(ctx, preview.ComposeProject, workDir); err != nil {
		log.Warn("compose down failed", "error", err)
	}

	if err := os.RemoveAll(workDir); err != nil {
		log.Warn("failed to remove workspace", "error", err)
	}

	// Delete old bot comment before posting removal notice
	if preview.GithubCommentID > 0 {
		_ = s.commenter.DeleteComment(ctx, req.RepoOwner, req.RepoName, preview.GithubCommentID, pat)
	}

	preview.Status = StatusDeleted
	_ = s.repo.Update(ctx, *preview)

	_, err = s.commenter.CreateComment(ctx, req.RepoOwner, req.RepoName, req.PRNumber, "### Preview removed", pat)
	if err != nil {
		log.Warn("failed to post removal comment", "error", err)
	}

	log.Info("preview deleted")
}

func (s *Service) setStatus(ctx context.Context, p *Preview, status Status, log *slog.Logger) {
	p.Status = status
	if err := s.repo.UpdateStatus(ctx, p.ID, status); err != nil {
		log.Error("failed to update status", "status", status, "error", err)
	}
}

func (s *Service) failPreview(ctx context.Context, p *Preview, completedSteps int, stage, message, pat string, log *slog.Logger) {
	log.Error("preview failed", "stage", stage, "error", message)
	p.Status = StatusFailed
	p.ErrorStage = stage
	p.ErrorMessage = message
	_ = s.repo.Update(ctx, *p)

	meta := commentMeta{
		Owner:  p.RepoOwner,
		Repo:   p.RepoName,
		SHA:    p.HeadSHA,
		Branch: p.BranchName,
	}
	body := failedProgressComment(meta, completedSteps, stage, message)
	s.updateComment(ctx, p, body, pat, log)
}

func (s *Service) updateComment(ctx context.Context, p *Preview, body, pat string, log *slog.Logger) {
	if p.GithubCommentID > 0 {
		if err := s.commenter.UpdateComment(ctx, p.RepoOwner, p.RepoName, p.GithubCommentID, body, pat); err != nil {
			log.Warn("failed to update comment", "error", err)
		}
	}
}

// commentMeta holds the repo context needed to build GitHub links in comments.
type commentMeta struct {
	Owner        string
	Repo         string
	SHA          string // full SHA
	Branch       string
	PreviewURL   string
	AnimationURL string
}

func (m commentMeta) commitLink() string {
	return fmt.Sprintf("[`%s`](https://github.com/%s/%s/commit/%s)", m.SHA[:8], m.Owner, m.Repo, m.SHA)
}

func (m commentMeta) branchLink() string {
	return fmt.Sprintf("[`%s`](https://github.com/%s/%s/tree/%s)", m.Branch, m.Owner, m.Repo, m.Branch)
}

// progressComment builds a markdown comment with a checklist showing deployment progress.
// In-progress comments include an animation; the final "ready" comment does not.
func progressComment(title string, meta commentMeta, completedSteps int, statusLine string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", title)
	fmt.Fprintf(&b, "Commit %s\n\n", meta.commitLink())

	for i := 0; i < numSteps; i++ {
		if i < completedSteps {
			fmt.Fprintf(&b, "- [x] %s\n", stepLabels[i])
		} else {
			fmt.Fprintf(&b, "- [ ] %s\n", stepLabels[i])
		}
	}

	if completedSteps < numSteps && meta.AnimationURL != "" {
		fmt.Fprintf(&b, "\n<table><tr><td><img src=\"%s\" width=\"64\" height=\"64\" /></td><td valign=\"middle\">%s</td></tr></table>", meta.AnimationURL, statusLine)
	} else {
		fmt.Fprintf(&b, "\n%s", statusLine)
	}

	return b.String()
}

// failedProgressComment builds a markdown comment showing which step failed.
func failedProgressComment(meta commentMeta, completedSteps int, stage, message string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Preview failed\n\n")
	fmt.Fprintf(&b, "Commit %s\n\n", meta.commitLink())

	for i := 0; i < numSteps; i++ {
		if i < completedSteps {
			fmt.Fprintf(&b, "- [x] %s\n", stepLabels[i])
		} else {
			fmt.Fprintf(&b, "- [ ] %s\n", stepLabels[i])
		}
	}

	fmt.Fprintf(&b, "\nFailed at `%s`:\n\n```\n%s\n```", stage, message)
	return b.String()
}
