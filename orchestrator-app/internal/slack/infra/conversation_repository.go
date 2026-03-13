package infra

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	agent "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/agent/domain"
)

// ConversationRepository persists and queries agent conversations.
type ConversationRepository struct {
	db *sql.DB
}

// NewConversationRepository creates a new conversation repository.
func NewConversationRepository(db *sql.DB) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// UpsertConversation inserts or updates a conversation record.
// On conflict, it updates last_active_at, and only overwrites summary/linked_issue
// if the new values are non-empty/non-zero.
func (r *ConversationRepository) UpsertConversation(ctx context.Context, conv agent.Conversation) error {
	id := uuid.New().String()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_conversations (
			id, channel_id, thread_ts, summary, user_name, last_active_at,
			linked_issue, repo_owner, repo_name
		) VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?)
		ON CONFLICT(channel_id, thread_ts) DO UPDATE SET
			last_active_at = excluded.last_active_at,
			summary        = CASE WHEN excluded.summary != '' THEN excluded.summary ELSE agent_conversations.summary END,
			user_name      = CASE WHEN excluded.user_name != '' THEN excluded.user_name ELSE agent_conversations.user_name END,
			linked_issue   = COALESCE(NULLIF(excluded.linked_issue, 0), agent_conversations.linked_issue)`,
		id, conv.ChannelID, conv.ThreadTs, conv.Summary, conv.UserName,
		conv.LastActiveAt, conv.LinkedIssue, conv.RepoOwner, conv.RepoName,
	)
	if err != nil {
		return fmt.Errorf("upsert conversation: %w", err)
	}
	return nil
}

// ListRecentConversations returns conversations in the given channel from the last N days,
// ordered by most recent activity first.
func (r *ConversationRepository) ListRecentConversations(ctx context.Context, channelID string, days int) ([]agent.Conversation, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := r.db.QueryContext(ctx, `
		SELECT channel_id, thread_ts, summary, user_name, last_active_at,
		       COALESCE(linked_issue, 0), repo_owner, repo_name
		FROM agent_conversations
		WHERE channel_id = ? AND last_active_at >= ?
		ORDER BY last_active_at DESC`,
		channelID, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	defer rows.Close()

	var convs []agent.Conversation
	for rows.Next() {
		var c agent.Conversation
		if err := rows.Scan(
			&c.ChannelID, &c.ThreadTs, &c.Summary, &c.UserName,
			&c.LastActiveAt, &c.LinkedIssue, &c.RepoOwner, &c.RepoName,
		); err != nil {
			return nil, fmt.Errorf("scan conversation: %w", err)
		}
		convs = append(convs, c)
	}
	return convs, rows.Err()
}
