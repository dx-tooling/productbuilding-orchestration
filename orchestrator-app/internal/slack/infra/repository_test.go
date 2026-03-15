package infra

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	db, err := database.Connect(":memory:")
	if err != nil {
		t.Fatalf("failed to connect to test db: %v", err)
	}

	// Run migrations
	if err := RunTestMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	return db
}

func TestRepository_SaveThread(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:            "test-id-1",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1234567890.123456",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	// Test saving new thread
	err := repo.SaveThread(ctx, thread)
	if err != nil {
		t.Errorf("SaveThread() error = %v", err)
	}
}

func TestRepository_FindThread_ByIssue(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save a thread first
	thread := &domain.SlackThread{
		ID:            "test-id-2",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1234567890.123456",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Test finding by issue
	found, err := repo.FindThread(ctx, "example-org", "test-repo", 42)
	if err != nil {
		t.Errorf("FindThread() error = %v", err)
		return
	}

	if found == nil {
		t.Error("FindThread() returned nil, expected thread")
		return
	}

	if found.ID != thread.ID {
		t.Errorf("ID = %v, want %v", found.ID, thread.ID)
	}
	if found.SlackChannel != thread.SlackChannel {
		t.Errorf("SlackChannel = %v, want %v", found.SlackChannel, thread.SlackChannel)
	}
	if found.SlackThreadTs != thread.SlackThreadTs {
		t.Errorf("SlackThreadTs = %v, want %v", found.SlackThreadTs, thread.SlackThreadTs)
	}
}

func TestRepository_FindThread_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Try to find non-existent thread
	found, err := repo.FindThread(ctx, "example-org", "test-repo", 999)
	if err == nil {
		t.Error("FindThread() expected error for non-existent thread, got nil")
	}
	if found != nil {
		t.Error("FindThread() expected nil for non-existent thread")
	}
}

func TestRepository_FindThread_ByPR(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save a PR thread
	thread := &domain.SlackThread{
		ID:            "test-id-3",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubPRID:    123,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1234567890.123456",
		ThreadType:    "pull_request",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Find by PR
	found, err := repo.FindThreadByPR(ctx, "example-org", "test-repo", 123)
	if err != nil {
		t.Errorf("FindThreadByPR() error = %v", err)
		return
	}

	if found == nil {
		t.Error("FindThreadByPR() returned nil, expected thread")
		return
	}

	if found.ID != thread.ID {
		t.Errorf("ID = %v, want %v", found.ID, thread.ID)
	}
	if found.ThreadType != "pull_request" {
		t.Errorf("ThreadType = %v, want pull_request", found.ThreadType)
	}
}

func TestRepository_DuplicatePrevention(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save first thread
	thread1 := &domain.SlackThread{
		ID:            "test-id-4",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1234567890.123456",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, thread1); err != nil {
		t.Fatalf("SaveThread() first error = %v", err)
	}

	// Try to save duplicate (same repo + issue number)
	thread2 := &domain.SlackThread{
		ID:            "test-id-5",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42, // Same issue number!
		SlackChannel:  "#productbuilding-test-2",
		SlackThreadTs: "9999999999.999999",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	err := repo.SaveThread(ctx, thread2)
	if err == nil {
		t.Error("SaveThread() expected error for duplicate, got nil")
	}
}

func TestRepository_FindThreadBySlackTs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:            "test-id-ts-1",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1111111111.111111",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Find by Slack thread timestamp
	found, err := repo.FindThreadBySlackTs(ctx, "1111111111.111111")
	if err != nil {
		t.Fatalf("FindThreadBySlackTs() error = %v", err)
	}

	if found.ID != thread.ID {
		t.Errorf("ID = %v, want %v", found.ID, thread.ID)
	}
	if found.GithubIssueID != 42 {
		t.Errorf("GithubIssueID = %v, want 42", found.GithubIssueID)
	}
	if found.RepoOwner != "example-org" {
		t.Errorf("RepoOwner = %v, want example-org", found.RepoOwner)
	}
}

func TestRepository_FindThreadBySlackTs_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	found, err := repo.FindThreadBySlackTs(ctx, "9999999999.999999")
	if err == nil {
		t.Error("FindThreadBySlackTs() expected error for non-existent thread, got nil")
	}
	if found != nil {
		t.Error("FindThreadBySlackTs() expected nil for non-existent thread")
	}
}

func TestRepository_FindThreadBySlackTs_ReturnsNewestMapping(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save first mapping: issue #41, created earlier
	thread1 := &domain.SlackThread{
		ID:            "test-id-old",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 41,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1111111111.111111",
		ThreadType:    "issue",
		CreatedAt:     time.Now().Add(-1 * time.Hour),
		UpdatedAt:     time.Now().Add(-1 * time.Hour),
	}
	if err := repo.SaveThread(ctx, thread1); err != nil {
		t.Fatalf("SaveThread(#41) error = %v", err)
	}

	// Save second mapping: issue #49, created later, same slack_thread_ts
	thread2 := &domain.SlackThread{
		ID:            "test-id-new",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 49,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1111111111.111111",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := repo.SaveThread(ctx, thread2); err != nil {
		t.Fatalf("SaveThread(#49) error = %v", err)
	}

	// FindThreadBySlackTs should return the newest mapping (#49)
	found, err := repo.FindThreadBySlackTs(ctx, "1111111111.111111")
	if err != nil {
		t.Fatalf("FindThreadBySlackTs() error = %v", err)
	}
	if found.GithubIssueID != 49 {
		t.Errorf("Expected most recent issue #49, got #%d", found.GithubIssueID)
	}
	if found.ID != "test-id-new" {
		t.Errorf("Expected ID test-id-new, got %s", found.ID)
	}
}

func TestRepository_UpdateThread(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save initial thread
	thread := &domain.SlackThread{
		ID:            "test-id-6",
		RepoOwner:     "example-org",
		RepoName:      "test-repo",
		GithubIssueID: 42,
		SlackChannel:  "#productbuilding-test",
		SlackThreadTs: "1234567890.123456",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Update thread timestamp
	thread.SlackThreadTs = "9999999999.999999"
	thread.UpdatedAt = time.Now()

	// Note: We might need an Update method, or we could use ON CONFLICT UPDATE
	// For now, let's test that we can find the original
	found, err := repo.FindThread(ctx, "example-org", "test-repo", 42)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}

	if found.SlackThreadTs != "1234567890.123456" {
		t.Errorf("SlackThreadTs = %v, want original value", found.SlackThreadTs)
	}
}
