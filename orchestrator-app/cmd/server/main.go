package main

import (
	"log/slog"
	"net/http"
	"os"

	// Agent vertical
	agentdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/agent/domain"

	// Dashboard vertical
	dashboardweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/dashboard/web"

	// GitHub vertical
	githubdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/domain"
	githubweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/web"

	// Preview vertical
	previewdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	previewinfra "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/infra"
	previewweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/web"

	// Slack vertical
	slackdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
	slackinfra "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/infra"
	slackweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/web"

	// Platform
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/config"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/logging"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/server"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

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
	registry := targets.NewRegistry()
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
	fireworksClient := agentdomain.NewFireworksClient(cfg.FireworksAPIKey)
	githubAdapter := agentdomain.NewGitHubClientAdapter(githubClient)
	toolExecutor := agentdomain.NewToolExecutor(githubAdapter)
	slackAdapter := agentdomain.NewSlackClientAdapter(slackClient)
	agentRunner := agentdomain.NewAgent(fireworksClient, toolExecutor, slackAdapter, cfg.FireworksModel)

	// ── Build HTTP Routes ──────────────────────────────────────────────
	mux := http.NewServeMux()

	// Register vertical routes
	dashboardweb.RegisterRoutes(mux, previewService)
	previewweb.RegisterRoutes(mux, previewService)
	githubweb.RegisterRoutes(mux, registry, previewService, slackNotifier)

	// Register Slack Events API routes (agent-driven @mention handling)
	slackHandler := slackweb.NewHandler(agentRunner, slackRepo, slackRepo, slackClient, registry, cfg.SlackSigningSecret, cfg.SlackWorkspace)
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
