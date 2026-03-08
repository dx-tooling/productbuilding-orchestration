CREATE TABLE previews (
    id                  TEXT PRIMARY KEY,
    repo_owner          TEXT NOT NULL,
    repo_name           TEXT NOT NULL,
    pr_number           INTEGER NOT NULL,
    branch_name         TEXT NOT NULL,
    head_sha            TEXT NOT NULL,
    preview_url         TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    compose_project     TEXT NOT NULL,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_successful_sha TEXT,
    error_stage         TEXT,
    error_message       TEXT,
    github_comment_id   INTEGER,
    UNIQUE(repo_owner, repo_name, pr_number)
);
