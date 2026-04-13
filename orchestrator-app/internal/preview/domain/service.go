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

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// SlackNotifier defines the interface for sending Slack notifications
type SlackNotifier interface {
	Notify(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) error
}

// Deploy step indices for the progress checklist.
const (
	stepDownload   = 0
	stepContainers = 1
	stepMigrations = 2
	stepHealth     = 3
	stepTLS        = 4
	stepPostDeploy = 5
	numSteps       = 6
)

var stepLabels = [numSteps]string{
	"Download source",
	"Build and start containers",
	"Run database migrations",
	"Health check",
	"TLS certificate",
	"Post-deploy setup",
}

type Service struct {
	repo               Repository
	downloader         SourceDownloader
	compose            ComposeManager
	healthChecker      HealthChecker
	commenter          PRCommenter
	notifier           SlackNotifier
	targetRegistry     *targets.Registry
	slackThreadChecker SlackThreadChecker
	previewDomain      string
	workspaceDir       string

	// Per-PR mutex to prevent concurrent operations on the same preview.
	locksMu sync.Mutex
	locks   map[string]*sync.Mutex

	// Track ongoing deployments to allow cancellation
	deploymentsMu sync.Mutex
	deployments   map[string]context.CancelFunc
}

// ServiceOption configures optional Service dependencies.
type ServiceOption func(*Service)

// WithSlackThreadChecker enables minimal PR comments when Slack is tracking the feature.
func WithSlackThreadChecker(checker SlackThreadChecker) ServiceOption {
	return func(s *Service) { s.slackThreadChecker = checker }
}

func NewService(
	repo Repository,
	downloader SourceDownloader,
	compose ComposeManager,
	healthChecker HealthChecker,
	commenter PRCommenter,
	notifier SlackNotifier,
	targetRegistry *targets.Registry,
	previewDomain string,
	workspaceDir string,
	opts ...ServiceOption,
) *Service {
	s := &Service{
		repo:           repo,
		downloader:     downloader,
		compose:        compose,
		healthChecker:  healthChecker,
		commenter:      commenter,
		notifier:       notifier,
		targetRegistry: targetRegistry,
		previewDomain:  previewDomain,
		workspaceDir:   workspaceDir,
		locks:          make(map[string]*sync.Mutex),
		deployments:    make(map[string]context.CancelFunc),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Service) getLock(key string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	if _, ok := s.locks[key]; !ok {
		s.locks[key] = &sync.Mutex{}
	}
	return s.locks[key]
}

// getDeploymentKey returns a unique key for a PR deployment
func (s *Service) getDeploymentKey(req DeployRequest) string {
	return fmt.Sprintf("%s/%s#%d", req.RepoOwner, req.RepoName, req.PRNumber)
}

// cancelExistingDeployment cancels any ongoing deployment for the same PR
func (s *Service) cancelExistingDeployment(key string) {
	s.deploymentsMu.Lock()
	defer s.deploymentsMu.Unlock()

	if cancel, exists := s.deployments[key]; exists && cancel != nil {
		slog.Info("cancelling existing deployment", "key", key)
		cancel()
		delete(s.deployments, key)
	}
}

// registerDeployment tracks a new deployment with its cancel function
func (s *Service) registerDeployment(key string, cancel context.CancelFunc) {
	s.deploymentsMu.Lock()
	defer s.deploymentsMu.Unlock()
	s.deployments[key] = cancel
}

// unregisterDeployment removes a deployment from tracking
func (s *Service) unregisterDeployment(key string) {
	s.deploymentsMu.Lock()
	defer s.deploymentsMu.Unlock()
	delete(s.deployments, key)
}

func (s *Service) ListPreviews(ctx context.Context) ([]Preview, error) {
	return s.repo.ListActive(ctx)
}

func (s *Service) GetPreview(ctx context.Context, repoOwner, repoName string, prNumber int) (*Preview, error) {
	return s.repo.FindByRepoPR(ctx, repoOwner, repoName, prNumber)
}

// GetPreviewLogs streams logs from a preview based on its logging configuration.
func (s *Service) GetPreviewLogs(ctx context.Context, repoOwner, repoName string, prNumber int, tail int, follow bool, w io.Writer) error {
	preview, err := s.repo.FindByRepoPR(ctx, repoOwner, repoName, prNumber)
	if err != nil {
		return fmt.Errorf("preview not found: %w", err)
	}

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)

	// Parse the contract to get logging configuration
	contract, err := ParseContract(workDir)
	if err != nil {
		return fmt.Errorf("failed to parse contract: %w", err)
	}

	// Determine logging configuration
	logType := "docker"
	logService := contract.Compose.Service
	logPath := ""

	if contract.Logging != nil {
		logType = contract.Logging.Type
		if contract.Logging.Service != "" {
			logService = contract.Logging.Service
		}
		logPath = contract.Logging.Path
	}

	// Stream logs based on type
	switch logType {
	case "file":
		if logPath == "" {
			return fmt.Errorf("logging.path is required when logging.type is 'file'")
		}
		return s.compose.LogsFromFile(ctx, preview.ComposeProject, logService, workDir, logPath, tail, follow, w)
	case "docker", "":
		return s.compose.Logs(ctx, preview.ComposeProject, logService, tail, follow, w)
	default:
		return fmt.Errorf("unsupported logging type: %s", logType)
	}
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
		LogsURL:      fmt.Sprintf("https://api.%s/previews/%s/%s/%d/logs", s.previewDomain, req.RepoOwner, req.RepoName, req.PRNumber),
		AnimationURL: "https://raw.githubusercontent.com/dx-tooling/assets/refs/heads/main/productbuilding/crane-building-animation-128x128.gif",
	}

	// Check if Slack is tracking this PR — use minimal comments to reduce noise
	slackTracking := s.slackThreadChecker != nil && s.slackThreadChecker.HasThread(ctx, req.RepoOwner, req.RepoName, req.PRNumber)

	// 1. Delete ALL previous bot comments and post new acknowledgment (before acquiring mutex)
	// This keeps the PR clean by removing old/aborted preview comments
	if err := s.commenter.DeleteAllBotComments(ctx, req.RepoOwner, req.RepoName, req.PRNumber, pat); err != nil {
		log.Warn("failed to clean up old bot comments", "error", err)
	}

	var ackBody string
	if slackTracking {
		ackBody = "<!-- productbuilding-orchestrator -->\nPreview deploying — status tracked in Slack."
	} else {
		ackBody = progressComment("Preview deploying", meta, 0, "Queued, waiting to start...")
	}
	commentID, err := s.commenter.CreateComment(ctx, req.RepoOwner, req.RepoName, req.PRNumber, ackBody, pat)
	if err != nil {
		log.Warn("failed to post ack comment", "error", err)
	}

	// Look up existing preview record for data reuse
	existing, _ := s.repo.FindByRepoPR(ctx, req.RepoOwner, req.RepoName, req.PRNumber)

	lockKey := fmt.Sprintf("%s/%s#%d", req.RepoOwner, req.RepoName, req.PRNumber)
	mu := s.getLock(lockKey)
	mu.Lock()
	defer mu.Unlock()

	// Cancel any existing deployment for this PR and create new cancellable context
	deploymentKey := s.getDeploymentKey(req)
	s.cancelExistingDeployment(deploymentKey)
	deployCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.registerDeployment(deploymentKey, cancel)
	defer s.unregisterDeployment(deploymentKey)

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

	if err := s.repo.Upsert(deployCtx, preview); err != nil {
		log.Error("failed to upsert preview", "error", err)
		return
	}

	// 3. Download source (status: building)
	s.setStatus(deployCtx, &preview, StatusBuilding, log)
	if !slackTracking {
		s.updateComment(deployCtx, &preview,
			progressComment("Preview deploying", meta, 0, "Downloading source..."),
			pat, log)
	}

	workDir := filepath.Join(s.workspaceDir, preview.ComposeProject)
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0755); err != nil {
		s.failPreview(deployCtx, &preview, stepDownload, "workspace", fmt.Sprintf("create workspace: %v", err), pat, log)
		return
	}

	if _, err := s.downloader.DownloadSource(deployCtx, req.RepoOwner, req.RepoName, req.HeadSHA, pat, workDir); err != nil {
		s.failPreview(deployCtx, &preview, stepDownload, "download", fmt.Sprintf("download source: %v", err), pat, log)
		return
	}

	// 4. Parse preview contract
	contract, err := ParseContract(workDir)
	if err != nil {
		s.failPreview(deployCtx, &preview, stepDownload+1, "contract", fmt.Sprintf("parse contract: %v", err), pat, log)
		return
	}

	// Add user-facing note from contract to meta
	meta.UserFacingNote = contract.UserFacingNote

	// 5. Generate compose override with Traefik labels
	routerName := fmt.Sprintf("%s-pr-%d", req.RepoName, req.PRNumber)
	host := fmt.Sprintf("%s-pr-%d.%s", req.RepoName, req.PRNumber, s.previewDomain)

	composeFile := contract.Compose.File
	overridePath, err := s.compose.GenerateOverride(workDir, contract.Compose.Service, routerName, host, contract.Runtime.InternalPort)
	if err != nil {
		s.failPreview(deployCtx, &preview, stepDownload+1, "override", fmt.Sprintf("generate override: %v", err), pat, log)
		return
	}
	overrideRel, _ := filepath.Rel(workDir, overridePath)

	// 6. Docker compose up (status: deploying)
	s.setStatus(deployCtx, &preview, StatusDeploying, log)
	if !slackTracking {
		s.updateComment(deployCtx, &preview,
			progressComment("Preview deploying", meta, stepDownload+1, "Building and starting containers..."),
			pat, log)
	}

	// Expose the PAT as GITHUB_TOKEN so repo contracts can use it for authenticated
	// operations (e.g. composer, npm, go modules). The contract decides how to use it.
	var composeEnv []string
	if pat != "" {
		composeEnv = []string{"GITHUB_TOKEN=" + pat}
	}

	if err := s.compose.Up(deployCtx, preview.ComposeProject, workDir, []string{composeFile, overrideRel}, composeEnv); err != nil {
		s.failPreview(deployCtx, &preview, stepContainers, "compose_up", fmt.Sprintf("compose up: %v", err), pat, log)
		return
	}

	// 7. Run database migrations (if configured)
	if contract.Database != nil && contract.Database.MigrateCommand != "" {
		if !slackTracking {
			s.updateComment(deployCtx, &preview,
				progressComment("Preview deploying", meta, stepContainers+1, "Running database migrations..."),
				pat, log)
		}

		migrateCmd := []string{"sh", "-c", contract.Database.MigrateCommand}
		if err := s.compose.Exec(deployCtx, preview.ComposeProject, contract.Compose.Service, workDir, migrateCmd); err != nil {
			s.failPreview(deployCtx, &preview, stepMigrations, "migrations", fmt.Sprintf("database migrations: %v", err), pat, log)
			return
		}
	}

	// 8. Health check
	if !slackTracking {
		s.updateComment(deployCtx, &preview,
			progressComment("Preview deploying", meta, stepMigrations+1, "Running health check..."),
			pat, log)
	}

	containerName := fmt.Sprintf("%s-%s-1", preview.ComposeProject, contract.Compose.Service)
	healthURL := fmt.Sprintf("http://%s:%d%s", containerName, contract.Runtime.InternalPort, contract.Runtime.HealthcheckPath)
	timeout := time.Duration(contract.Runtime.StartupTimeout) * time.Second

	if err := s.healthChecker.WaitForHealthy(deployCtx, healthURL, timeout); err != nil {
		s.failPreview(deployCtx, &preview, stepHealth, "healthcheck", fmt.Sprintf("health check: %v", err), pat, log)
		return
	}

	// 8. TLS certificate readiness (wait for Traefik to provision via Let's Encrypt)
	if !slackTracking {
		s.updateComment(deployCtx, &preview,
			progressComment("Preview deploying", meta, stepHealth+1, "Waiting for TLS certificate..."),
			pat, log)
	}

	tlsTimeout := 120 * time.Second
	if err := s.healthChecker.WaitForTLS(deployCtx, meta.PreviewURL, tlsTimeout); err != nil {
		s.failPreview(deployCtx, &preview, stepTLS, "tls", fmt.Sprintf("TLS readiness: %v", err), pat, log)
		return
	}

	// 9. Run post-deploy commands (if configured)
	if len(contract.PostDeployCommands) > 0 {
		if !slackTracking {
			s.updateComment(deployCtx, &preview,
				progressComment("Preview deploying", meta, stepTLS+1, "Running post-deploy commands..."),
				pat, log)
		}

		for _, cmd := range contract.PostDeployCommands {
			service := cmd.Service
			if service == "" {
				service = contract.Compose.Service
			}

			desc := cmd.Description
			if desc == "" {
				desc = cmd.Command
			}

			log.Info("running post-deploy command", "service", service, "cmd", cmd.Command, "desc", desc)

			execCmd := []string{"sh", "-c", cmd.Command}
			if err := s.compose.Exec(deployCtx, preview.ComposeProject, service, workDir, execCmd); err != nil {
				s.failPreview(deployCtx, &preview, stepPostDeploy, "post_deploy", fmt.Sprintf("post-deploy command '%s': %v", desc, err), pat, log)
				return
			}
		}
	}

	// 10. Ready!
	preview.Status = StatusReady
	preview.LastSuccessfulSHA = req.HeadSHA
	preview.ErrorStage = ""
	preview.ErrorMessage = ""
	_ = s.repo.Update(deployCtx, preview)

	if slackTracking {
		s.updateComment(deployCtx, &preview,
			fmt.Sprintf("<!-- productbuilding-orchestrator -->\nPreview: %s", meta.PreviewURL),
			pat, log)
	} else {
		s.updateComment(deployCtx, &preview,
			progressComment("Preview ready", meta, numSteps, fmt.Sprintf("**[%s](%s)**  •  %s", meta.PreviewURL, meta.PreviewURL, meta.logsLink())),
			pat, log)
	}

	// Notify Slack that preview is ready
	if target, ok := s.targetRegistry.Get(preview.RepoOwner, preview.RepoName); ok {
		s.notifySlack(deployCtx, &preview, slackfacade.EventPRReady, "ready", target, meta.UserFacingNote)
	}

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

	_, err = s.commenter.CreateComment(ctx, req.RepoOwner, req.RepoName, req.PRNumber, "<!-- productbuilding-orchestrator -->\n### Preview removed", pat)
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

	// Notify Slack that preview failed
	if target, ok := s.targetRegistry.Get(p.RepoOwner, p.RepoName); ok {
		s.notifySlack(ctx, p, slackfacade.EventPRFailed, stage+": "+message, target, "")
	}
}

func (s *Service) updateComment(ctx context.Context, p *Preview, body, pat string, log *slog.Logger) {
	if p.GithubCommentID > 0 {
		if err := s.commenter.UpdateComment(ctx, p.RepoOwner, p.RepoName, p.GithubCommentID, body, pat); err != nil {
			log.Warn("failed to update comment", "error", err)
		}
	}
}

// notifySlack sends a Slack notification for preview status updates
func (s *Service) notifySlack(ctx context.Context, p *Preview, eventType slackfacade.EventType, status string, target targets.TargetConfig, userNote string) {
	if s.notifier == nil {
		return
	}

	// Silently skip if no Slack config for this target
	if target.SlackChannel == "" || target.SlackBotToken == "" {
		return
	}

	logsURL := fmt.Sprintf("https://api.%s/previews/%s/%s/%d/logs", s.previewDomain, p.RepoOwner, p.RepoName, p.PRNumber)

	event := slackfacade.NotificationEvent{
		Type:        eventType,
		RepoOwner:   p.RepoOwner,
		RepoName:    p.RepoName,
		IssueNumber: p.PRNumber,
		Title:       fmt.Sprintf("Preview for %s", p.BranchName),
		Status:      status,
		PreviewURL:  p.PreviewURL,
		LogsURL:     logsURL,
		Author:      "ProductBuilder",
		UserNote:    userNote,
	}

	if err := s.notifier.Notify(ctx, event, target); err != nil {
		slog.Warn("failed to send slack notification", "error", err, "repo", p.RepoOwner+"/"+p.RepoName, "pr", p.PRNumber)
	}
}

// commentMeta holds the repo context needed to build GitHub links in comments.
type commentMeta struct {
	Owner          string
	Repo           string
	SHA            string // full SHA
	Branch         string
	PreviewURL     string
	LogsURL        string
	AnimationURL   string
	UserFacingNote string // Optional note from the preview contract
}

func (m commentMeta) commitLink() string {
	return fmt.Sprintf("[`%s`](https://github.com/%s/%s/commit/%s)", m.SHA[:8], m.Owner, m.Repo, m.SHA)
}

func (m commentMeta) branchLink() string {
	return fmt.Sprintf("[%s](https://github.com/%s/%s/tree/%s)", m.Branch, m.Owner, m.Repo, m.Branch)
}

func (m commentMeta) logsLink() string {
	return fmt.Sprintf("[View Logs](%s)", m.LogsURL)
}

// progressComment builds a markdown comment with a checklist showing deployment progress.
// In-progress comments include an animation; the final "ready" comment does not.
func progressComment(title string, meta commentMeta, completedSteps int, statusLine string) string {
	var b strings.Builder
	// Add unique marker to identify our bot comments for cleanup
	fmt.Fprintf(&b, "<!-- productbuilding-orchestrator -->\n")
	fmt.Fprintf(&b, "### %s\n\n", title)
	fmt.Fprintf(&b, "Commit %s\n\n", meta.commitLink())

	for i := 0; i < numSteps; i++ {
		if i < completedSteps {
			fmt.Fprintf(&b, "- [x] %s\n", stepLabels[i])
		} else {
			fmt.Fprintf(&b, "- [ ] %s\n", stepLabels[i])
		}
	}

	// Add user-facing note if present
	if meta.UserFacingNote != "" {
		fmt.Fprintf(&b, "\n> **Note:** %s\n", meta.UserFacingNote)
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
	// Add unique marker to identify our bot comments for cleanup
	fmt.Fprintf(&b, "<!-- productbuilding-orchestrator -->\n")
	fmt.Fprintf(&b, "### Preview failed\n\n")
	fmt.Fprintf(&b, "Commit %s\n\n", meta.commitLink())

	for i := 0; i < numSteps; i++ {
		if i < completedSteps {
			fmt.Fprintf(&b, "- [x] %s\n", stepLabels[i])
		} else {
			fmt.Fprintf(&b, "- [ ] %s\n", stepLabels[i])
		}
	}

	// Add user-facing note if present
	if meta.UserFacingNote != "" {
		fmt.Fprintf(&b, "\n> **Note:** %s\n", meta.UserFacingNote)
	}

	fmt.Fprintf(&b, "\nFailed at `%s`:\n\n```\n%s\n```", stage, message)
	return b.String()
}
