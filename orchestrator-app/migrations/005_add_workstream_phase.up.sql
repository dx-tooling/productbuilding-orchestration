ALTER TABLE slack_threads ADD COLUMN workstream_phase TEXT NOT NULL DEFAULT '';
ALTER TABLE slack_threads ADD COLUMN preview_notified_at DATETIME;
ALTER TABLE slack_threads ADD COLUMN feedback_relayed INTEGER NOT NULL DEFAULT 0;
