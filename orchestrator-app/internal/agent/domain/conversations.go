package domain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Conversation represents a tracked agent conversation in a Slack thread.
type Conversation struct {
	ChannelID    string
	ThreadTs     string
	Summary      string
	UserName     string
	LastActiveAt time.Time
	LinkedIssue  int
	RepoOwner    string
	RepoName     string
}

// ConversationLister retrieves recent conversations for a channel.
type ConversationLister interface {
	ListRecentConversations(ctx context.Context, channelID string, days int) ([]Conversation, error)
}

// ConversationRecorder persists conversation metadata.
type ConversationRecorder interface {
	UpsertConversation(ctx context.Context, conv Conversation) error
}

// SlackThreadLink builds a deep link to a Slack thread message.
func SlackThreadLink(workspace, channelID, threadTs string) string {
	// Slack deep links use the timestamp without the dot: p1234567890123456
	ts := strings.ReplaceAll(threadTs, ".", "")
	return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s", workspace, channelID, ts)
}

// TruncateSummary truncates text to maxLen, adding "…" if truncated.
func TruncateSummary(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 1 {
		return "…"
	}
	return text[:maxLen-1] + "…"
}
