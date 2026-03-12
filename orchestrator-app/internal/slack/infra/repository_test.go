package infra

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
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
		RepoOwner:     "luminor-project",
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
		RepoOwner:     "luminor-project",
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
	found, err := repo.FindThread(ctx, "luminor-project", "test-repo", 42)
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
	found, err := repo.FindThread(ctx, "luminor-project", "test-repo", 999)
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
		RepoOwner:     "luminor-project",
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
	found, err := repo.FindThreadByPR(ctx, "luminor-project", "test-repo", 123)
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
		RepoOwner:     "luminor-project",
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
		RepoOwner:     "luminor-project",
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

func TestRepository_UpdateThread(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Save initial thread
	thread := &domain.SlackThread{
		ID:            "test-id-6",
		RepoOwner:     "luminor-project",
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
	found, err := repo.FindThread(ctx, "luminor-project", "test-repo", 42)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}

	if found.SlackThreadTs != "1234567890.123456" {
		t.Errorf("SlackThreadTs = %v, want original value", found.SlackThreadTs)
	}
}
