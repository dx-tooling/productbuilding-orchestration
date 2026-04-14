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
	if err != nil {
		t.Errorf("SaveThread() duplicate should be silently ignored, got error: %v", err)
	}

	// First mapping should be preserved
	found, findErr := repo.FindThread(ctx, "example-org", "test-repo", 42)
	if findErr != nil {
		t.Fatalf("FindThread() error = %v", findErr)
	}
	if found.SlackThreadTs != "1234567890.123456" {
		t.Errorf("expected first mapping's thread_ts, got %s", found.SlackThreadTs)
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

func TestRepository_SaveThread_RoundTripsWorkstreamPhase(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	now := time.Now()
	thread := &domain.SlackThread{
		ID:                "test-ws-1",
		RepoOwner:         "example-org",
		RepoName:          "test-repo",
		GithubIssueID:     50,
		SlackChannel:      "#productbuilding-test",
		SlackThreadTs:     "1234567890.500000",
		ThreadType:        "issue",
		WorkstreamPhase:   domain.PhaseReview,
		PreviewNotifiedAt: &now,
		FeedbackRelayed:   true,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 50)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}

	if found.WorkstreamPhase != domain.PhaseReview {
		t.Errorf("WorkstreamPhase = %q, want %q", found.WorkstreamPhase, domain.PhaseReview)
	}
	if found.PreviewNotifiedAt == nil {
		t.Error("PreviewNotifiedAt should not be nil")
	}
	if !found.FeedbackRelayed {
		t.Error("FeedbackRelayed should be true")
	}
}

func TestRepository_SaveThread_NilPreviewNotifiedAt(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:              "test-ws-2",
		RepoOwner:       "example-org",
		RepoName:        "test-repo",
		GithubIssueID:   51,
		SlackChannel:    "#productbuilding-test",
		SlackThreadTs:   "1234567890.510000",
		ThreadType:      "issue",
		WorkstreamPhase: domain.PhaseOpen,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 51)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}

	if found.WorkstreamPhase != domain.PhaseOpen {
		t.Errorf("WorkstreamPhase = %q, want %q", found.WorkstreamPhase, domain.PhaseOpen)
	}
	if found.PreviewNotifiedAt != nil {
		t.Error("PreviewNotifiedAt should be nil")
	}
	if found.FeedbackRelayed {
		t.Error("FeedbackRelayed should be false")
	}
}

func TestRepository_UpdateWorkstreamPhase(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:              "test-ws-3",
		RepoOwner:       "example-org",
		RepoName:        "test-repo",
		GithubIssueID:   52,
		SlackChannel:    "#productbuilding-test",
		SlackThreadTs:   "1234567890.520000",
		ThreadType:      "issue",
		WorkstreamPhase: domain.PhaseOpen,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Update phase
	if err := repo.UpdateWorkstreamPhase(ctx, "1234567890.520000", domain.PhaseInProgress); err != nil {
		t.Fatalf("UpdateWorkstreamPhase() error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 52)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}
	if found.WorkstreamPhase != domain.PhaseInProgress {
		t.Errorf("WorkstreamPhase = %q, want %q", found.WorkstreamPhase, domain.PhaseInProgress)
	}
}

func TestRepository_SetPreviewNotified(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:              "test-ws-4",
		RepoOwner:       "example-org",
		RepoName:        "test-repo",
		GithubIssueID:   53,
		SlackChannel:    "#productbuilding-test",
		SlackThreadTs:   "1234567890.530000",
		ThreadType:      "issue",
		WorkstreamPhase: domain.PhaseInProgress,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	if err := repo.SetPreviewNotified(ctx, "1234567890.530000"); err != nil {
		t.Fatalf("SetPreviewNotified() error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 53)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}
	if found.PreviewNotifiedAt == nil {
		t.Error("PreviewNotifiedAt should be set after SetPreviewNotified")
	}
}

func TestRepository_SetFeedbackRelayed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:              "test-ws-5",
		RepoOwner:       "example-org",
		RepoName:        "test-repo",
		GithubIssueID:   54,
		SlackChannel:    "#productbuilding-test",
		SlackThreadTs:   "1234567890.540000",
		ThreadType:      "issue",
		WorkstreamPhase: domain.PhaseReview,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	if err := repo.SetFeedbackRelayed(ctx, "1234567890.540000", true); err != nil {
		t.Fatalf("SetFeedbackRelayed() error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 54)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}
	if !found.FeedbackRelayed {
		t.Error("FeedbackRelayed should be true after SetFeedbackRelayed")
	}
}

func TestRepository_ResetFeedbackRelayed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	thread := &domain.SlackThread{
		ID:              "test-ws-6",
		RepoOwner:       "example-org",
		RepoName:        "test-repo",
		GithubIssueID:   55,
		SlackChannel:    "#productbuilding-test",
		SlackThreadTs:   "1234567890.550000",
		ThreadType:      "issue",
		WorkstreamPhase: domain.PhaseRevision,
		FeedbackRelayed: true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := repo.SaveThread(ctx, thread); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	// Reset feedback relayed when new preview goes live
	if err := repo.SetFeedbackRelayed(ctx, "1234567890.550000", false); err != nil {
		t.Fatalf("SetFeedbackRelayed(false) error = %v", err)
	}

	found, err := repo.FindThread(ctx, "example-org", "test-repo", 55)
	if err != nil {
		t.Fatalf("FindThread() error = %v", err)
	}
	if found.FeedbackRelayed {
		t.Error("FeedbackRelayed should be false after reset")
	}
}

func TestRepository_PRMappingFoundAfterIssueMappingExists(t *testing.T) {
	// Reproduces the pr_ready lookup failure: an issue thread exists,
	// then a PR-only mapping is saved for the same Slack thread,
	// and FindThreadByNumber(prNumber) must find it.
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Step 1: Save issue thread (issue #106)
	issueThread := &domain.SlackThread{
		ID:            "issue-thread-1",
		RepoOwner:     "luminor-project",
		RepoName:      "luminor-core-go-playground",
		GithubIssueID: 106,
		SlackChannel:  "#productbuilding",
		SlackThreadTs: "1776190536.593119",
		ThreadType:    "issue",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := repo.SaveThread(ctx, issueThread); err != nil {
		t.Fatalf("SaveThread(issue) error = %v", err)
	}

	// Step 2: Save PR-only mapping (PR #107 → same Slack thread)
	// This is what the notifier does when it links a PR to an existing issue thread.
	prThread := &domain.SlackThread{
		ID:            "pr-thread-1",
		RepoOwner:     "luminor-project",
		RepoName:      "luminor-core-go-playground",
		GithubPRID:    107,
		SlackChannel:  "#productbuilding",
		SlackThreadTs: "1776190536.593119",
		ThreadType:    "pull_request",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := repo.SaveThread(ctx, prThread); err != nil {
		t.Fatalf("SaveThread(PR) error = %v", err)
	}

	// Step 3: FindThreadByNumber(107) must find the PR mapping
	found, err := repo.FindThreadByNumber(ctx, "luminor-project", "luminor-core-go-playground", 107)
	if err != nil {
		t.Fatalf("FindThreadByNumber(107) error = %v", err)
	}
	if found == nil {
		t.Fatal("FindThreadByNumber(107) returned nil — PR mapping not found")
	}
	if found.GithubPRID != 107 {
		t.Errorf("GithubPRID = %d, want 107", found.GithubPRID)
	}
	if found.SlackThreadTs != "1776190536.593119" {
		t.Errorf("SlackThreadTs = %s, want 1776190536.593119", found.SlackThreadTs)
	}
}

func TestRepository_MultiplePRMappingsForDifferentRepos(t *testing.T) {
	// Multiple PR-only mappings (GithubIssueID=0) must coexist without
	// UNIQUE constraint violations when they belong to different repos.
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	pr1 := &domain.SlackThread{
		ID:            "pr-a",
		RepoOwner:     "org",
		RepoName:      "repo-a",
		GithubPRID:    10,
		SlackChannel:  "#ch",
		SlackThreadTs: "111.111",
		ThreadType:    "pull_request",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	pr2 := &domain.SlackThread{
		ID:            "pr-b",
		RepoOwner:     "org",
		RepoName:      "repo-b",
		GithubPRID:    20,
		SlackChannel:  "#ch",
		SlackThreadTs: "222.222",
		ThreadType:    "pull_request",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := repo.SaveThread(ctx, pr1); err != nil {
		t.Fatalf("SaveThread(pr1) error = %v", err)
	}
	if err := repo.SaveThread(ctx, pr2); err != nil {
		t.Fatalf("SaveThread(pr2) error = %v", err)
	}

	found, err := repo.FindThreadByNumber(ctx, "org", "repo-b", 20)
	if err != nil {
		t.Fatalf("FindThreadByNumber(20) error = %v", err)
	}
	if found == nil {
		t.Fatal("FindThreadByNumber(20) returned nil")
	}
	if found.GithubPRID != 20 {
		t.Errorf("GithubPRID = %d, want 20", found.GithubPRID)
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

func TestRepository_UpdateWorkstreamPhase_NoThread_ReturnsNil(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	// Updating phase for a non-existent thread should be a no-op, not an error.
	// This happens when the agent responds to a new top-level message that has
	// no thread mapping yet.
	err := repo.UpdateWorkstreamPhase(ctx, "9999999999.999999", domain.PhaseIntake)
	if err != nil {
		t.Errorf("UpdateWorkstreamPhase() for missing thread should return nil, got: %v", err)
	}
}

func TestRepository_SetFeedbackRelayed_NoThread_ReturnsNil(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewSQLiteRepository(db)
	ctx := context.Background()

	err := repo.SetFeedbackRelayed(ctx, "9999999999.999999", true)
	if err != nil {
		t.Errorf("SetFeedbackRelayed() for missing thread should return nil, got: %v", err)
	}
}
