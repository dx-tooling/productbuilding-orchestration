package domain

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

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

// DeployPreview handles the full lifecycle: download → build → deploy → healthcheck.
// Runs synchronously; callers should invoke in a goroutine for async processing.
func (s *Service) DeployPreview(ctx context.Context, req DeployRequest, pat string) {
	log := slog.With("repo", req.RepoOwner+"/"+req.RepoName, "pr", req.PRNumber, "sha", req.HeadSHA[:8])

	// 1. Post acknowledgment comment immediately (before acquiring mutex)
	previewURL := fmt.Sprintf("https://%s-pr-%d.%s", req.RepoName, req.PRNumber, s.previewDomain)
	ackBody := fmt.Sprintf(
		"### Preview deployment queued\n\nCommit `%s` on `%s` → [%s](%s)\n\nWaiting to start...",
		req.HeadSHA[:8], req.Branch, previewURL, previewURL,
	)
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

	existing, _ := s.repo.FindByRepoPR(ctx, req.RepoOwner, req.RepoName, req.PRNumber)
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

	// Update ack comment to show we've started
	s.updateComment(ctx, &preview, fmt.Sprintf(
		"### Preview deploying\n\nCommit `%s` on `%s` → [%s](%s)\n\nDownloading source...",
		req.HeadSHA[:8], req.Branch, preview.PreviewURL, preview.PreviewURL,
	), pat, log)

	// 3. Download source (status: building)
	s.setStatus(ctx, &preview, StatusBuilding, log)

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)
	// Clean previous source to avoid stale files
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		s.failPreview(ctx, &preview, "workspace", fmt.Sprintf("create workspace: %v", err), pat, log)
		return
	}

	if _, err := s.downloader.DownloadSource(ctx, req.RepoOwner, req.RepoName, req.HeadSHA, pat, workDir); err != nil {
		s.failPreview(ctx, &preview, "download", fmt.Sprintf("download source: %v", err), pat, log)
		return
	}

	// 4. Parse preview contract
	contract, err := ParseContract(workDir)
	if err != nil {
		s.failPreview(ctx, &preview, "contract", fmt.Sprintf("parse contract: %v", err), pat, log)
		return
	}

	// 5. Generate compose override with Traefik labels
	routerName := fmt.Sprintf("%s-pr-%d", req.RepoName, req.PRNumber)
	host := fmt.Sprintf("%s-pr-%d.%s", req.RepoName, req.PRNumber, s.previewDomain)

	composeFile := contract.Compose.File
	overridePath, err := s.compose.GenerateOverride(workDir, contract.Compose.Service, routerName, host, contract.Runtime.InternalPort)
	if err != nil {
		s.failPreview(ctx, &preview, "override", fmt.Sprintf("generate override: %v", err), pat, log)
		return
	}
	// Make override path relative to workDir for docker compose -f
	overrideRel, _ := filepath.Rel(workDir, overridePath)

	// 6. Docker compose up (status: deploying)
	s.setStatus(ctx, &preview, StatusDeploying, log)
	s.updateComment(ctx, &preview, fmt.Sprintf(
		"### Preview deploying\n\nCommit `%s` on `%s` → [%s](%s)\n\nStarting containers...",
		req.HeadSHA[:8], req.Branch, preview.PreviewURL, preview.PreviewURL,
	), pat, log)

	if err := s.compose.Up(ctx, preview.ComposeProject, workDir, []string{composeFile, overrideRel}); err != nil {
		s.failPreview(ctx, &preview, "compose_up", fmt.Sprintf("compose up: %v", err), pat, log)
		return
	}

	// 7. Health check — reach the container via Docker network
	containerName := fmt.Sprintf("%s-%s-1", preview.ComposeProject, contract.Compose.Service)
	healthURL := fmt.Sprintf("http://%s:%d%s", containerName, contract.Runtime.InternalPort, contract.Runtime.HealthcheckPath)
	timeout := time.Duration(contract.Runtime.StartupTimeout) * time.Second

	if err := s.healthChecker.WaitForHealthy(ctx, healthURL, timeout); err != nil {
		s.failPreview(ctx, &preview, "healthcheck", fmt.Sprintf("health check: %v", err), pat, log)
		return
	}

	// 8. Ready!
	preview.Status = StatusReady
	preview.LastSuccessfulSHA = req.HeadSHA
	preview.ErrorStage = ""
	preview.ErrorMessage = ""
	_ = s.repo.Update(ctx, preview)

	readyComment := fmt.Sprintf(
		"### Preview ready\n\nCommit `%s` on `%s` → **[%s](%s)**",
		req.HeadSHA[:8], req.Branch, preview.PreviewURL, preview.PreviewURL,
	)
	s.updateComment(ctx, &preview, readyComment, pat, log)

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

func (s *Service) failPreview(ctx context.Context, p *Preview, stage, message, pat string, log *slog.Logger) {
	log.Error("preview failed", "stage", stage, "error", message)
	p.Status = StatusFailed
	p.ErrorStage = stage
	p.ErrorMessage = message
	_ = s.repo.Update(ctx, *p)

	failComment := fmt.Sprintf("### Preview failed\n\nFailed at stage `%s`:\n\n```\n%s\n```", stage, message)
	s.updateComment(ctx, p, failComment, pat, log)
}

func (s *Service) updateComment(ctx context.Context, p *Preview, body, pat string, log *slog.Logger) {
	if p.GithubCommentID > 0 {
		if err := s.commenter.UpdateComment(ctx, p.RepoOwner, p.RepoName, p.GithubCommentID, body, pat); err != nil {
			log.Warn("failed to update comment", "error", err)
		}
	}
}
