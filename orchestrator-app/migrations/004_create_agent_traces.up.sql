CREATE TABLE IF NOT EXISTS agent_traces (
    id TEXT PRIMARY KEY,
    repo_owner TEXT NOT NULL DEFAULT '',
    repo_name TEXT NOT NULL DEFAULT '',
    github_issue_id INTEGER NOT NULL DEFAULT 0,
    github_pr_id INTEGER NOT NULL DEFAULT 0,
    slack_channel TEXT NOT NULL DEFAULT '',
    slack_thread_ts TEXT NOT NULL DEFAULT '',
    user_name TEXT NOT NULL DEFAULT '',
    user_text TEXT NOT NULL DEFAULT '',
    trace_data TEXT NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_traces_repo_issue ON agent_traces(repo_owner, repo_name, github_issue_id);
CREATE INDEX IF NOT EXISTS idx_traces_slack ON agent_traces(slack_channel, slack_thread_ts);
CREATE INDEX IF NOT EXISTS idx_traces_created ON agent_traces(created_at);
