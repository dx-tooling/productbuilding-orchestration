CREATE TABLE agent_conversations (
    id              TEXT PRIMARY KEY,
    channel_id      TEXT NOT NULL,
    thread_ts       TEXT NOT NULL,
    summary         TEXT NOT NULL DEFAULT '',
    user_name       TEXT NOT NULL DEFAULT '',
    last_active_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    linked_issue    INTEGER,
    repo_owner      TEXT NOT NULL DEFAULT '',
    repo_name       TEXT NOT NULL DEFAULT '',
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_id, thread_ts)
);

CREATE INDEX idx_agent_conversations_channel_active
    ON agent_conversations(channel_id, last_active_at DESC);
