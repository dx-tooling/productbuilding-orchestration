package domain

import (
	"context"

	slackdomain "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// SlackClientAdapter wraps a slack domain Client to satisfy the agent's SlackThreadFetcher interface.
type SlackClientAdapter struct {
	client *slackdomain.Client
}

// NewSlackClientAdapter creates a new adapter.
func NewSlackClientAdapter(client *slackdomain.Client) *SlackClientAdapter {
	return &SlackClientAdapter{client: client}
}

func (a *SlackClientAdapter) GetThreadReplies(ctx context.Context, botToken, channel, threadTs string) ([]ThreadMessage, error) {
	msgs, err := a.client.GetThreadReplies(ctx, botToken, channel, threadTs)
	if err != nil {
		return nil, err
	}
	out := make([]ThreadMessage, len(msgs))
	for i, m := range msgs {
		out[i] = ThreadMessage{
			User:  m.User,
			Text:  m.Text,
			Ts:    m.Ts,
			BotID: m.BotID,
		}
	}
	return out, nil
}
