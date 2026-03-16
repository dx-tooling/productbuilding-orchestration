package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	// Agent vertical
	agentdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	agentinfra "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/infra"

	// Dashboard vertical
	dashboardweb "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/dashboard/web"

	// GitHub vertical
	githubdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
	githubweb "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/web"

	// Preview vertical
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	previewinfra "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/infra"
	previewweb "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/web"

	// Slack vertical
	slackdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
	slackinfra "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/infra"
	slackweb "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/web"

	// Platform
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/config"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/logging"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/server"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// dashboardTraceAdapter adapts agentinfra.TraceRepository to dashboardweb.TraceQuerier.
type dashboardTraceAdapter struct {
	repo *agentinfra.TraceRepository
}

func newDashboardTraceAdapter(repo *agentinfra.TraceRepository) *dashboardTraceAdapter {
	return &dashboardTraceAdapter{repo: repo}
}

func (a *dashboardTraceAdapter) FindByIssue(ctx context.Context, owner, repo string, issueID int) ([]dashboardweb.TraceResult, error) {
	records, err := a.repo.FindByIssue(ctx, owner, repo, issueID)
	if err != nil {
		return nil, err
	}
	return convertTraceRecords(records), nil
}

func (a *dashboardTraceAdapter) FindBySlackThread(ctx context.Context, channel, threadTs string) ([]dashboardweb.TraceResult, error) {
	records, err := a.repo.FindBySlackThread(ctx, channel, threadTs)
	if err != nil {
		return nil, err
	}
	return convertTraceRecords(records), nil
}

func convertTraceRecords(records []agentinfra.TraceRecord) []dashboardweb.TraceResult {
	results := make([]dashboardweb.TraceResult, len(records))
	for i, r := range records {
		results[i] = dashboardweb.TraceResult{
			ID:            r.ID,
			RepoOwner:     r.RepoOwner,
			RepoName:      r.RepoName,
			GithubIssueID: r.GithubIssueID,
			SlackChannel:  r.SlackChannel,
			SlackThreadTs: r.SlackThreadTs,
			UserName:      r.UserName,
			UserText:      r.UserText,
			TraceData:     r.TraceData,
			Error:         r.Error,
			CreatedAt:     r.CreatedAt,
		}
	}
	return results
}

// slackTraceAdapter adapts agentinfra.TraceRepository to slackweb.TraceSaver.
type slackTraceAdapter struct {
	repo *agentinfra.TraceRepository
}

func newSlackTraceAdapter(repo *agentinfra.TraceRepository) *slackTraceAdapter {
	return &slackTraceAdapter{repo: repo}
}

func (a *slackTraceAdapter) SaveTrace(ctx context.Context, req slackweb.TraceSaveRequest) error {
	return a.repo.SaveTrace(ctx, agentinfra.TraceRecord{
		RepoOwner:     req.RepoOwner,
		RepoName:      req.RepoName,
		GithubIssueID: req.GithubIssueID,
		SlackChannel:  req.SlackChannel,
		SlackThreadTs: req.SlackThreadTs,
		UserName:      req.UserName,
		UserText:      req.UserText,
		TraceData:     req.TraceData,
		Error:         req.Error,
	})
}

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Setup logging
	logging.Setup(cfg.AppEnv)

	// Connect to database
	db, err := database.Connect(cfg.DatabasePath)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Run migrations
	migrationsFS := os.DirFS("migrations")
	if err := database.RunMigrations(db, migrationsFS); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// ── Load Target Registry ───────────────────────────────────────────
	registry := targets.NewRegistry(cfg.SlackChannelPrefix)
	if err := registry.LoadFromFile(cfg.TargetsConfigPath); err != nil {
		slog.Warn("failed to load targets config", "path", cfg.TargetsConfigPath, "error", err)
	} else {
		slog.Info("targets loaded", "count", registry.Count())
	}

	// ── Build Infrastructure ───────────────────────────────────────────
	previewRepo := previewinfra.NewSQLiteRepository(db)
	githubClient := githubdomain.NewClient()
	composeRunner := previewinfra.NewComposeRunner()
	healthChecker := previewinfra.NewHealthChecker()

	// ── Build Slack Notifier (bot token loaded per target from config) ────
	slackRepo := slackinfra.NewSQLiteRepository(db)
	slackDebouncer := slackinfra.NewDebouncer()
	slackClient := slackdomain.NewClient()
	slackNotifier := slackdomain.NewNotifier(slackClient, slackRepo, slackDebouncer)

	// ── Build Preview Vertical ─────────────────────────────────────────
	previewService := previewdomain.NewService(
		previewRepo,
		githubClient,  // SourceDownloader
		composeRunner, // ComposeManager
		healthChecker, // HealthChecker
		githubClient,  // PRCommenter
		slackNotifier, // SlackNotifier
		registry,      // TargetRegistry
		cfg.PreviewDomain,
		cfg.WorkspaceDir,
	)

	// ── Build Agent ────────────────────────────────────────────────────
	fireworksClient := agentdomain.NewFireworksClientWithConfig(
		cfg.FireworksAPIKey,
		time.Duration(cfg.LLMRequestTimeout)*time.Second,
		agentdomain.RetryConfig{
			MaxRetries: cfg.LLMMaxRetries,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
		},
	)
	githubAdapter := agentdomain.NewGitHubClientAdapter(githubClient)
	toolExecutor := agentdomain.NewToolExecutor(githubAdapter)
	slackAdapter := agentdomain.NewSlackClientAdapter(slackClient)
	convRepo := slackinfra.NewConversationRepository(db)
	agentRunner := agentdomain.NewOrchestrator(fireworksClient, toolExecutor, slackAdapter, cfg.FireworksModel,
		agentdomain.OrchestratorConfig{
			ConversationLister: convRepo,
			Workspace:          cfg.SlackWorkspace,
			TokenBudget:        agentdomain.TokenBudget{Total: cfg.AgentTokenBudget, IssueMaxTokens: 1000, ThreadMaxMessages: 20},
		},
	)

	// ── Build Trace Repository ────────────────────────────────────────
	traceRepo := agentinfra.NewTraceRepository(db)

	// ── Build HTTP Routes ──────────────────────────────────────────────
	mux := http.NewServeMux()

	// Register vertical routes
	dashboardweb.RegisterRoutes(mux, previewService, newDashboardTraceAdapter(traceRepo))
	previewweb.RegisterRoutes(mux, previewService)
	githubweb.RegisterRoutes(mux, registry, previewService, slackNotifier)

	// Register Slack Events API routes (agent-driven @mention handling)
	slackHandler := slackweb.NewHandler(agentRunner, slackRepo, slackRepo, convRepo, slackClient, registry, cfg.SlackSigningSecret, cfg.SlackWorkspace)
	slackHandler.SetAgentTimeout(time.Duration(cfg.AgentRunTimeout) * time.Second)
	slackHandler.SetTraceSaver(newSlackTraceAdapter(traceRepo))
	slackweb.RegisterRoutes(mux, slackHandler)

	// ── Health Endpoints (outside application middleware) ───────────────
	topMux := http.NewServeMux()
	topMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	topMux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	topMux.Handle("/", mux)

	// ── Start Server ───────────────────────────────────────────────────
	addr := ":" + cfg.Port
	slog.Info("orchestrator starting", "addr", addr, "env", cfg.AppEnv, "preview_domain", cfg.PreviewDomain)
	server.ListenAndServe(topMux, addr)
}
