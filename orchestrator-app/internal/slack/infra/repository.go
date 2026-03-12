package infra

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/database"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// Repository defines the interface for Slack thread persistence
type Repository interface {
	SaveThread(ctx context.Context, thread *domain.SlackThread) error
	FindThread(ctx context.Context, repoOwner, repoName string, issueNumber int) (*domain.SlackThread, error)
	FindThreadByPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*domain.SlackThread, error)
	FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*domain.SlackThread, error)
}

// SQLiteRepository implements Repository using SQLite
type SQLiteRepository struct {
	db *sql.DB
}

// NewSQLiteRepository creates a new SQLite-backed repository
func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

// SaveThread persists or updates a SlackThread in the database.
// NULLIF ensures 0-valued IDs are stored as NULL so the UNIQUE constraints
// on (repo_owner, repo_name, github_issue_id) and (repo_owner, repo_name, github_pr_id)
// allow multiple issues (pr_id NULL) and multiple PRs (issue_id NULL).
func (r *SQLiteRepository) SaveThread(ctx context.Context, thread *domain.SlackThread) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO slack_threads (
			id, repo_owner, repo_name, github_issue_id, github_pr_id,
			slack_channel, slack_thread_ts, slack_parent_ts, thread_type,
			created_at, updated_at
		) VALUES (?, ?, ?, NULLIF(?, 0), NULLIF(?, 0), ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			github_issue_id = NULLIF(excluded.github_issue_id, 0),
			github_pr_id    = NULLIF(excluded.github_pr_id, 0),
			thread_type     = excluded.thread_type,
			updated_at      = excluded.updated_at`,
		thread.ID,
		thread.RepoOwner,
		thread.RepoName,
		thread.GithubIssueID,
		thread.GithubPRID,
		thread.SlackChannel,
		thread.SlackThreadTs,
		thread.SlackParentTs,
		thread.ThreadType,
		thread.CreatedAt,
		thread.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save slack thread: %w", err)
	}
	return nil
}

// FindThread finds a thread by repo owner, name, and issue number
func (r *SQLiteRepository) FindThread(ctx context.Context, repoOwner, repoName string, issueNumber int) (*domain.SlackThread, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
		       slack_channel, slack_thread_ts, slack_parent_ts, thread_type,
		       created_at, updated_at
		FROM slack_threads
		WHERE repo_owner = ? AND repo_name = ? AND github_issue_id = ?`,
		repoOwner, repoName, issueNumber,
	)

	return scanThread(row)
}

// FindThreadByPR finds a thread by repo owner, name, and PR number
func (r *SQLiteRepository) FindThreadByPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*domain.SlackThread, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
		       slack_channel, slack_thread_ts, slack_parent_ts, thread_type,
		       created_at, updated_at
		FROM slack_threads
		WHERE repo_owner = ? AND repo_name = ? AND github_pr_id = ?`,
		repoOwner, repoName, prNumber,
	)

	return scanThread(row)
}

// FindThreadByNumber finds a thread by either issue ID OR PR ID
// This is needed because in GitHub, PRs are also issues and share the same numbering
func (r *SQLiteRepository) FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*domain.SlackThread, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
		       slack_channel, slack_thread_ts, slack_parent_ts, thread_type,
		       created_at, updated_at
		FROM slack_threads
		WHERE repo_owner = ? AND repo_name = ? AND (github_issue_id = ? OR github_pr_id = ?)`,
		repoOwner, repoName, number, number,
	)

	return scanThread(row)
}

// scanThread scans a database row into a SlackThread
func scanThread(row *sql.Row) (*domain.SlackThread, error) {
	var thread domain.SlackThread
	var issueID, prID sql.NullInt64
	err := row.Scan(
		&thread.ID,
		&thread.RepoOwner,
		&thread.RepoName,
		&issueID,
		&prID,
		&thread.SlackChannel,
		&thread.SlackThreadTs,
		&thread.SlackParentTs,
		&thread.ThreadType,
		&thread.CreatedAt,
		&thread.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("thread not found")
		}
		return nil, fmt.Errorf("scan thread: %w", err)
	}
	thread.GithubIssueID = int(issueID.Int64)
	thread.GithubPRID = int(prID.Int64)
	return &thread, nil
}

// RunTestMigrations runs migrations for testing (exposed for test use)
func RunTestMigrations(db *sql.DB) error {
	migrationsFS := os.DirFS("../../../migrations")
	return database.RunMigrations(db, migrationsFS)
}
