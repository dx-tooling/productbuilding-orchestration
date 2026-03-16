package infra

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/database"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Connect(":memory:")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	migrationsFS := os.DirFS("../../../migrations")
	if err := database.RunMigrations(db, migrationsFS); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	return db
}

func TestTraceRepository_SaveAndFindByIssue(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	repo := NewTraceRepository(db)

	record := TraceRecord{
		RepoOwner:     "luminor-project",
		RepoName:      "playground",
		GithubIssueID: 57,
		SlackChannel:  "C0AL8824SBH",
		SlackThreadTs: "1773578348.731309",
		UserName:      "Alice",
		UserText:      "what's the CI status?",
		TraceData:     `{"routing":{"output":"researcher"}}`,
	}
	err := repo.SaveTrace(context.Background(), record)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	traces, err := repo.FindByIssue(context.Background(), "luminor-project", "playground", 57)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].UserText != "what's the CI status?" {
		t.Errorf("expected user text, got %q", traces[0].UserText)
	}
	if traces[0].TraceData != `{"routing":{"output":"researcher"}}` {
		t.Errorf("expected trace data, got %q", traces[0].TraceData)
	}
	if traces[0].ID == "" {
		t.Error("expected ID to be set")
	}
}

func TestTraceRepository_FindByIssue_NewestFirst(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	repo := NewTraceRepository(db)

	// Insert with explicit timestamps to guarantee ordering
	for i, text := range []string{"first", "second", "third"} {
		ts := time.Date(2026, 3, 16, 10, 0, i, 0, time.UTC).Format("2006-01-02 15:04:05")
		_, err := db.Exec(`
			INSERT INTO agent_traces (id, repo_owner, repo_name, github_issue_id, user_text, trace_data, created_at)
			VALUES (?, 'org', 'repo', 10, ?, '{}', ?)`,
			fmt.Sprintf("id-%d", i), text, ts)
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	traces, err := repo.FindByIssue(context.Background(), "org", "repo", 10)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(traces) != 3 {
		t.Fatalf("expected 3 traces, got %d", len(traces))
	}
	if traces[0].UserText != "third" {
		t.Errorf("expected newest first ('third'), got %q", traces[0].UserText)
	}
}

func TestTraceRepository_FindBySlackThread(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	repo := NewTraceRepository(db)

	record := TraceRecord{
		SlackChannel:  "C0AL8824SBH",
		SlackThreadTs: "1773578348.731309",
		UserText:      "check this",
		TraceData:     `{"steps":[]}`,
	}
	if err := repo.SaveTrace(context.Background(), record); err != nil {
		t.Fatalf("save: %v", err)
	}

	traces, err := repo.FindBySlackThread(context.Background(), "C0AL8824SBH", "1773578348.731309")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(traces))
	}
	if traces[0].UserText != "check this" {
		t.Errorf("expected 'check this', got %q", traces[0].UserText)
	}
}

func TestTraceRepository_SaveTrace_CleansUpOldTraces(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	repo := NewTraceRepository(db)

	// Insert an old trace directly with a timestamp > 24h ago
	_, err := db.Exec(`
		INSERT INTO agent_traces (id, repo_owner, repo_name, github_issue_id, user_text, trace_data, created_at)
		VALUES ('old-id', 'org', 'repo', 1, 'old trace', '{}', datetime('now', '-25 hours'))`)
	if err != nil {
		t.Fatalf("insert old: %v", err)
	}

	// Save a new trace — should trigger cleanup
	newRecord := TraceRecord{
		RepoOwner:     "org",
		RepoName:      "repo",
		GithubIssueID: 2,
		UserText:      "new trace",
		TraceData:     "{}",
	}
	if err := repo.SaveTrace(context.Background(), newRecord); err != nil {
		t.Fatalf("save: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT count(*) FROM agent_traces WHERE id = 'old-id'").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected old trace to be cleaned up, but found %d", count)
	}

	if err := db.QueryRow("SELECT count(*) FROM agent_traces").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 trace remaining, got %d", count)
	}
}
