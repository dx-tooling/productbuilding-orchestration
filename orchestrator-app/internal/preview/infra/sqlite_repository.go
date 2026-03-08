package infra

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) *SQLiteRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) Upsert(ctx context.Context, p domain.Preview) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO previews (id, repo_owner, repo_name, pr_number, branch_name, head_sha, preview_url, status, compose_project, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(repo_owner, repo_name, pr_number) DO UPDATE SET
			branch_name = excluded.branch_name,
			head_sha = excluded.head_sha,
			status = excluded.status,
			updated_at = ?`,
		p.ID, p.RepoOwner, p.RepoName, p.PRNumber, p.BranchName, p.HeadSHA,
		p.PreviewURL, string(p.Status), p.ComposeProject, p.CreatedAt, p.UpdatedAt,
		time.Now(),
	)
	if err != nil {
		return fmt.Errorf("upsert preview: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) FindByRepoPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*domain.Preview, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, repo_owner, repo_name, pr_number, branch_name, head_sha, preview_url,
		       status, compose_project, created_at, updated_at,
		       COALESCE(last_successful_sha, ''), COALESCE(error_stage, ''), COALESCE(error_message, ''),
		       COALESCE(github_comment_id, 0)
		FROM previews
		WHERE repo_owner = ? AND repo_name = ? AND pr_number = ?`,
		repoOwner, repoName, prNumber,
	)
	return scanPreview(row)
}

func (r *SQLiteRepository) ListActive(ctx context.Context) ([]domain.Preview, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, repo_owner, repo_name, pr_number, branch_name, head_sha, preview_url,
		       status, compose_project, created_at, updated_at,
		       COALESCE(last_successful_sha, ''), COALESCE(error_stage, ''), COALESCE(error_message, ''),
		       COALESCE(github_comment_id, 0)
		FROM previews
		WHERE status != ?
		ORDER BY updated_at DESC`,
		string(domain.StatusDeleted),
	)
	if err != nil {
		return nil, fmt.Errorf("query active previews: %w", err)
	}
	defer rows.Close()

	var previews []domain.Preview
	for rows.Next() {
		p, err := scanPreviewRows(rows)
		if err != nil {
			return nil, err
		}
		previews = append(previews, *p)
	}
	return previews, rows.Err()
}

func (r *SQLiteRepository) UpdateStatus(ctx context.Context, id string, status domain.Status) error {
	_, err := r.db.ExecContext(ctx,
		"UPDATE previews SET status = ?, updated_at = ? WHERE id = ?",
		string(status), time.Now(), id,
	)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanPreview(s scanner) (*domain.Preview, error) {
	var p domain.Preview
	var status string
	err := s.Scan(
		&p.ID, &p.RepoOwner, &p.RepoName, &p.PRNumber, &p.BranchName, &p.HeadSHA,
		&p.PreviewURL, &status, &p.ComposeProject, &p.CreatedAt, &p.UpdatedAt,
		&p.LastSuccessfulSHA, &p.ErrorStage, &p.ErrorMessage, &p.GithubCommentID,
	)
	if err != nil {
		return nil, fmt.Errorf("scan preview: %w", err)
	}
	p.Status = domain.Status(status)
	return &p, nil
}

func scanPreviewRows(rows *sql.Rows) (*domain.Preview, error) {
	return scanPreview(rows)
}
