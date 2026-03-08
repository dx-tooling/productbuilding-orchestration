package main

import (
	"log/slog"
	"net/http"
	"os"

	// Dashboard vertical
	dashboardweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/dashboard/web"

	// GitHub vertical
	githubweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/github/web"

	// Preview vertical
	previewdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	previewinfra "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/infra"
	previewweb "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/web"

	// Platform
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/config"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/logging"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/server"
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

	// ── Build Preview Vertical ─────────────────────────────────────────
	previewRepo := previewinfra.NewSQLiteRepository(db)
	previewService := previewdomain.NewService(previewRepo)

	// ── Build HTTP Routes ──────────────────────────────────────────────
	mux := http.NewServeMux()

	// Register vertical routes
	dashboardweb.RegisterRoutes(mux, previewService)
	previewweb.RegisterRoutes(mux, previewService)
	githubweb.RegisterRoutes(mux, "" /* webhook secret — loaded per-target in Phase 3 */)

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
