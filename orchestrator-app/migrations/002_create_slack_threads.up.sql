CREATE TABLE slack_threads (
    id              TEXT PRIMARY KEY,
    repo_owner      TEXT NOT NULL,
    repo_name       TEXT NOT NULL,
    github_issue_id INTEGER,
    github_pr_id    INTEGER,
    slack_channel   TEXT NOT NULL,
    slack_thread_ts TEXT NOT NULL,
    slack_parent_ts TEXT,
    thread_type     TEXT NOT NULL CHECK(thread_type IN ('issue', 'pull_request')),
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    
    UNIQUE(repo_owner, repo_name, github_issue_id),
    UNIQUE(repo_owner, repo_name, github_pr_id)
);

CREATE INDEX idx_slack_threads_repo ON slack_threads(repo_owner, repo_name);
CREATE INDEX idx_slack_threads_channel ON slack_threads(slack_channel);
