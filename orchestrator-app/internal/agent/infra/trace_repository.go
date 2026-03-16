package infra

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TraceRecord is the database representation of an agent execution trace.
type TraceRecord struct {
	ID            string
	RepoOwner     string
	RepoName      string
	GithubIssueID int
	GithubPRID    int
	SlackChannel  string
	SlackThreadTs string
	UserName      string
	UserText      string
	TraceData     string
	Error         string
	CreatedAt     time.Time
}

// TraceRepository persists and queries agent execution traces.
type TraceRepository struct {
	db *sql.DB
}

// NewTraceRepository creates a new trace repository.
func NewTraceRepository(db *sql.DB) *TraceRepository {
	return &TraceRepository{db: db}
}

// SaveTrace inserts a trace record and cleans up records older than 24 hours.
func (r *TraceRepository) SaveTrace(ctx context.Context, record TraceRecord) error {
	record.ID = uuid.New().String()

	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_traces (
			id, repo_owner, repo_name, github_issue_id, github_pr_id,
			slack_channel, slack_thread_ts, user_name, user_text,
			trace_data, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.RepoOwner, record.RepoName,
		record.GithubIssueID, record.GithubPRID,
		record.SlackChannel, record.SlackThreadTs,
		record.UserName, record.UserText,
		record.TraceData, record.Error,
	)
	if err != nil {
		return fmt.Errorf("insert trace: %w", err)
	}

	// Cleanup traces older than 24 hours
	_, _ = r.db.ExecContext(ctx, `DELETE FROM agent_traces WHERE created_at < datetime('now', '-24 hours')`)

	return nil
}

// FindByIssue returns traces matching a repo + issue number, newest first.
// If owner and repo are empty, matches across all repos.
func (r *TraceRepository) FindByIssue(ctx context.Context, owner, repo string, issueID int) ([]TraceRecord, error) {
	if owner == "" && repo == "" {
		return r.query(ctx, `
			SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
			       slack_channel, slack_thread_ts, user_name, user_text,
			       trace_data, error, created_at
			FROM agent_traces
			WHERE github_issue_id = ?
			ORDER BY created_at DESC`, issueID)
	}
	return r.query(ctx, `
		SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
		       slack_channel, slack_thread_ts, user_name, user_text,
		       trace_data, error, created_at
		FROM agent_traces
		WHERE repo_owner = ? AND repo_name = ? AND github_issue_id = ?
		ORDER BY created_at DESC`, owner, repo, issueID)
}

// FindBySlackThread returns traces matching a Slack channel + thread timestamp, newest first.
func (r *TraceRepository) FindBySlackThread(ctx context.Context, channel, threadTs string) ([]TraceRecord, error) {
	return r.query(ctx, `
		SELECT id, repo_owner, repo_name, github_issue_id, github_pr_id,
		       slack_channel, slack_thread_ts, user_name, user_text,
		       trace_data, error, created_at
		FROM agent_traces
		WHERE slack_channel = ? AND slack_thread_ts = ?
		ORDER BY created_at DESC`, channel, threadTs)
}

func (r *TraceRepository) query(ctx context.Context, query string, args ...interface{}) ([]TraceRecord, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query traces: %w", err)
	}
	defer rows.Close()

	var traces []TraceRecord
	for rows.Next() {
		var t TraceRecord
		if err := rows.Scan(
			&t.ID, &t.RepoOwner, &t.RepoName, &t.GithubIssueID, &t.GithubPRID,
			&t.SlackChannel, &t.SlackThreadTs, &t.UserName, &t.UserText,
			&t.TraceData, &t.Error, &t.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan trace: %w", err)
		}
		traces = append(traces, t)
	}
	return traces, rows.Err()
}
