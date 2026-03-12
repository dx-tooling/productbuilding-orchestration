package domain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// SlackClient defines the interface for Slack API operations (matches *Client)
type SlackClient interface {
	PostMessage(ctx context.Context, botToken, channel string, msg MessageBlock) (string, error)
	PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error
	AddReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error
	RemoveReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error
}

// ThreadRepository defines the interface for thread persistence
type ThreadRepository interface {
	SaveThread(ctx context.Context, thread *SlackThread) error
	FindThread(ctx context.Context, repoOwner, repoName string, issueNumber int) (*SlackThread, error)
	FindThreadByPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*SlackThread, error)
}

// Debouncer batches rapid events
type Debouncer interface {
	Debounce(key string, wait time.Duration, fn func())
}

// Notifier sends notifications to Slack with debouncing and emoji reactions
type Notifier struct {
	client     SlackClient
	repository ThreadRepository
	debouncer  Debouncer
	buffer     map[string]*slackfacade.NotificationEvent // key: channel#issue -> latest event
	reactions  map[string]string                         // threadTs -> current emoji
	mu         sync.Mutex
}

// NewNotifier creates a new Slack notifier with the given dependencies
func NewNotifier(client SlackClient, repository ThreadRepository, debouncer Debouncer) *Notifier {
	return &Notifier{
		client:     client,
		repository: repository,
		debouncer:  debouncer,
		buffer:     make(map[string]*slackfacade.NotificationEvent),
		reactions:  make(map[string]string),
	}
}

// Notify sends a notification to Slack (debounced)
func (n *Notifier) Notify(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) error {
	// Silently skip if no Slack config
	if target.SlackChannel == "" || target.SlackBotToken == "" {
		return nil
	}

	key := fmt.Sprintf("%s#%d", target.SlackChannel, event.IssueNumber)

	// Buffer the event (keep only latest)
	n.mu.Lock()
	n.buffer[key] = &event
	currentEmoji := n.reactions[event.ThreadTs]
	n.mu.Unlock()

	// Handle emoji reactions immediately (not debounced)
	if event.Emoji != "" && event.Emoji != currentEmoji && event.ThreadTs != "" {
		if currentEmoji != "" {
			n.client.RemoveReaction(ctx, target.SlackBotToken, target.SlackChannel, event.ThreadTs, currentEmoji)
		}
		n.client.AddReaction(ctx, target.SlackBotToken, target.SlackChannel, event.ThreadTs, event.Emoji)
		n.reactions[event.ThreadTs] = event.Emoji
	}

	// Debounce the message posting
	n.debouncer.Debounce(key, 2*time.Second, func() {
		n.flush(ctx, key, target)
	})

	return nil
}

// flush sends the buffered notification
func (n *Notifier) flush(ctx context.Context, key string, target targets.TargetConfig) {
	n.mu.Lock()
	event := n.buffer[key]
	delete(n.buffer, key)
	n.mu.Unlock()

	if event == nil {
		return
	}

	// Find or create thread
	var thread *SlackThread
	var err error

	if event.IsPR() {
		thread, err = n.repository.FindThreadByPR(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)
	} else {
		thread, err = n.repository.FindThread(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)
	}

	newThread := false
	if err != nil || thread == nil {
		newThread = true
		// Create new thread
		parentMsg := formatParentMessage(*event)
		parentTs, err := n.client.PostMessage(ctx, target.SlackBotToken, target.SlackChannel, parentMsg)
		if err != nil {
			slog.Warn("failed to create slack thread",
				"error", err,
				"channel", target.SlackChannel,
				"repo", event.RepoOwner+"/"+event.RepoName,
				"issue", event.IssueNumber,
			)
			return
		}

		var issueID, prID int
		if event.IsPR() {
			prID = event.IssueNumber
		} else {
			issueID = event.IssueNumber
		}

		thread = &SlackThread{
			ID:            uuid.New().String(),
			RepoOwner:     event.RepoOwner,
			RepoName:      event.RepoName,
			GithubIssueID: issueID,
			GithubPRID:    prID,
			SlackChannel:  target.SlackChannel,
			SlackThreadTs: parentTs,
			SlackParentTs: parentTs,
			ThreadType:    event.IssueOrPR(),
		}

		if err := n.repository.SaveThread(ctx, thread); err != nil {
			slog.Warn("failed to save slack thread", "error", err)
			// Continue anyway - we can still post to the thread
		}

		// Update event with thread timestamp for future reactions
		n.mu.Lock()
		n.reactions[parentTs] = event.Emoji
		n.mu.Unlock()
	}

	// Post update to thread (only if not the first message creating the thread)
	if newThread {
		return // Parent message already posted as update
	}

	updateMsg := formatEventMessage(*event)
	if err := n.client.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, updateMsg); err != nil {
		slog.Warn("failed to post to slack thread",
			"error", err,
			"channel", thread.SlackChannel,
			"thread", thread.SlackThreadTs,
		)
	}
}

// formatParentMessage creates the initial thread message
func formatParentMessage(event slackfacade.NotificationEvent) MessageBlock {
	emoji := "📝"
	if event.IsPR() {
		emoji = "🔀"
	}

	text := fmt.Sprintf("%s *%s* #%d: %s\n<%s|View on GitHub>",
		emoji,
		event.IssueOrPR(),
		event.IssueNumber,
		event.Title,
		event.GitHubURL(),
	)

	return MessageBlock{Text: text}
}

// formatEventMessage formats an update message for a thread
func formatEventMessage(event slackfacade.NotificationEvent) MessageBlock {
	var text string

	switch event.Type {
	case slackfacade.EventPRReady:
		lines := []string{
			fmt.Sprintf("✅ *Preview ready*"),
			fmt.Sprintf("🔗 <%s|Open Preview>", event.PreviewURL),
		}
		if event.LogsURL != "" {
			lines = append(lines, fmt.Sprintf("📋 <%s|View Logs>", event.LogsURL))
		}
		if event.UserNote != "" {
			lines = append(lines, fmt.Sprintf("> *Note:* %s", event.UserNote))
		}
		text = strings.Join(lines, "\n")

	case slackfacade.EventPRFailed:
		text = fmt.Sprintf("❌ *Preview failed*\n> Stage: `%s`", event.Status)

	case slackfacade.EventPROpened, slackfacade.EventIssueOpened:
		text = fmt.Sprintf("👤 Opened by @%s", event.Author)

	case slackfacade.EventCommentAdded:
		preview := truncate(event.Body, 150)
		url := event.CommentURL()
		if url != "" {
			text = fmt.Sprintf("💬 @%s: %s\n<%s|View full comment>", event.Author, preview, url)
		} else {
			text = fmt.Sprintf("💬 @%s: %s", event.Author, preview)
		}

	case slackfacade.EventPRMerged:
		text = "🎉 *Merged* — Preview will be removed shortly"

	case slackfacade.EventIssueClosed:
		text = "✅ *Closed*"

	case slackfacade.EventPRClosed:
		text = "🔒 *Closed* — Preview removed"

	default:
		text = fmt.Sprintf("📢 Update: %s", event.Type)
	}

	return MessageBlock{Text: text}
}

// truncate limits text length with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
