package main

import (
	"context"
	"fmt"
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

	// Feature context
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"

	// Slack vertical
	slackdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
	slackinfra "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/infra"
	slackweb "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/web"

	// Targetadmin vertical (GitHub-side ingress reconciler)
	targetadmindomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/targetadmin/domain"
	targetadmininfra "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/targetadmin/infra"

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

// eventAgentRunnerAdapter adapts agentdomain.Orchestrator to slackdomain.EventAgentRunner.
type eventAgentRunnerAdapter struct {
	runner *agentdomain.Orchestrator
}

func (a *eventAgentRunnerAdapter) RunForEvent(ctx context.Context, req slackdomain.EventRunRequest) (slackdomain.EventRunResponse, error) {
	resp, err := a.runner.Run(ctx, agentdomain.RunRequest{
		ChannelID:       req.ChannelID,
		ThreadTs:        req.ThreadTs,
		UserText:        req.UserText,
		BotUserID:       req.BotUserID,
		Target:          req.Target,
		WorkstreamPhase: req.WorkstreamPhase,
	})
	if err != nil {
		return slackdomain.EventRunResponse{}, err
	}
	return slackdomain.EventRunResponse{Text: resp.Text}, nil
}

// slackThreadCheckerAdapter adapts slackinfra.SQLiteRepository to previewdomain.SlackThreadChecker.
type slackThreadCheckerAdapter struct {
	repo *slackinfra.SQLiteRepository
}

func (a *slackThreadCheckerAdapter) HasThread(ctx context.Context, owner, repo string, prNumber int) bool {
	thread, err := a.repo.FindThreadByPR(ctx, owner, repo, prNumber)
	return err == nil && thread != nil
}

// prStateAdapter satisfies previewdomain.PullRequestStateChecker by delegating
// to githubdomain.Client.GetPR. Lives here so the github vertical doesn't
// need to know about the preview vertical's interface shape.
type prStateAdapter struct{ c *githubdomain.Client }

func (a *prStateAdapter) IsPullRequestOpen(ctx context.Context, owner, repo string, prNumber int, pat string) (bool, error) {
	pr, err := a.c.GetPR(ctx, owner, repo, prNumber, pat)
	if err != nil {
		return false, err
	}
	return pr.State == "open", nil
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

	// Build feature context assembler for enriching notifications
	featureAssembler := featurecontext.NewAssembler(
		featurecontext.NewGitHubIssueAdapter(githubClient),
		featurecontext.NewGitHubPRAdapter(githubClient),
		featurecontext.NewActionsCheckRunAdapter(githubClient),
		featurecontext.NewPreviewAdapter(previewRepo),
	)

	slackNotifier := slackdomain.NewNotifier(slackClient, slackRepo, slackDebouncer, featureAssembler)

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
		previewdomain.WithSlackThreadChecker(&slackThreadCheckerAdapter{slackRepo}),
		previewdomain.WithPRStateChecker(&prStateAdapter{githubClient}),
	)

	// ── Reconcile Previews After Restart ───────────────────────────────
	// Runs in a goroutine so HTTP serving comes up immediately. Sweeps
	// orphan in-flight rows to failed and redeploys any "ready" preview
	// whose compose project is no longer running. Closed PRs are reaped.
	// See previewdomain.Service.ReconcileActive for full semantics.
	go previewService.ReconcileActive(context.Background())

	// ── Reconcile GitHub-side target ingress ───────────────────────────
	// Ensures every registered target has a webhook on GitHub pointing here
	// with the correct secret/events, plus an up-to-date FIREWORKS_API_KEY
	// Actions secret (for OpenCode workflows on the target repo). Uses each
	// target's PAT, so onboarding a target in any GitHub org is purely a
	// tfvars edit + apply + deploy — no provider aliases required.
	adminClient := targetadmininfra.NewGitHubAdminClient(githubClient)
	adminReconciler := targetadmindomain.NewReconciler(
		registry,
		adminClient,
		fmt.Sprintf("https://api.%s/webhook", cfg.PreviewDomain),
	)
	go adminReconciler.ReconcileAll(context.Background())

	// ── Build Agent ────────────────────────────────────────────────────
	llmClient, err := agentdomain.NewLLMClient(cfg.LLMConfig())
	if err != nil {
		slog.Error("failed to create LLM client", "error", err)
		os.Exit(1)
	}
	githubAdapter := agentdomain.NewGitHubClientAdapter(githubClient)
	toolExecutor := agentdomain.NewToolExecutor(githubAdapter)
	slackAdapter := agentdomain.NewSlackClientAdapter(slackClient)
	convRepo := slackinfra.NewConversationRepository(db)
	agentRunner := agentdomain.NewOrchestrator(llmClient, toolExecutor, slackAdapter,
		agentdomain.OrchestratorConfig{
			ConversationLister: convRepo,
			Workspace:          cfg.SlackWorkspace,
			TokenBudget:        agentdomain.TokenBudget{Total: cfg.AgentTokenBudget, IssueMaxTokens: 1000, ThreadMaxMessages: 20},
		},
	)

	// ── Build Event Agent Invoker ────────────────────────────────────
	eventAgentRunner := &eventAgentRunnerAdapter{runner: agentRunner}
	eventInvoker := slackdomain.NewEventAgentInvoker(eventAgentRunner, slackRepo, slackClient, 5*time.Second)

	// Wire the event narrator into the notifier so preview-ready/failed events
	// get conversational LLM narration instead of template messages.
	slackNotifier.SetEventNarrator(eventAgentRunner)

	// ── Build Trace Repository ────────────────────────────────────────
	traceRepo := agentinfra.NewTraceRepository(db)

	// ── Build HTTP Routes ──────────────────────────────────────────────
	mux := http.NewServeMux()

	// Register vertical routes
	dashboardweb.RegisterRoutes(mux, previewService, newDashboardTraceAdapter(traceRepo))
	previewweb.RegisterRoutes(mux, previewService)
	githubweb.RegisterRoutes(mux, registry, previewService, slackNotifier, eventInvoker)

	// Register Slack Events API routes (agent-driven @mention handling)
	slackHandler := slackweb.NewHandler(agentRunner, slackRepo, slackRepo, convRepo, slackClient, registry, cfg.SlackSigningSecret, cfg.SlackWorkspace)
	slackHandler.SetAgentTimeout(time.Duration(cfg.AgentRunTimeout) * time.Second)
	slackHandler.SetTraceSaver(newSlackTraceAdapter(traceRepo))
	slackHandler.SetFeatureAssembler(featureAssembler)
	slackHandler.SetPhaseUpdater(slackRepo)
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
