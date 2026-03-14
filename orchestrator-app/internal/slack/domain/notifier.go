package domain

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
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
	// FindThreadByNumber searches by either issue ID or PR ID (they share numbers in GitHub)
	FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*SlackThread, error)
}

// Debouncer batches rapid events
type Debouncer interface {
	Debounce(key string, wait time.Duration, fn func())
}

// pendingFlush separates status events (deduped by overwrite) from comments (queued).
type pendingFlush struct {
	status   *slackfacade.NotificationEvent   // latest lifecycle/status event (overwrite)
	comments []*slackfacade.NotificationEvent // all comments in arrival order (append)
}

// Notifier sends notifications to Slack with debouncing and emoji reactions
type Notifier struct {
	client     SlackClient
	repository ThreadRepository
	debouncer  Debouncer
	pending    map[string]*pendingFlush // key: channel#issue -> two-lane buffer
	reactions  map[string]string        // threadTs -> current emoji
	retryWait  time.Duration            // wait before retry lookups (default 5s)
	mu         sync.Mutex
}

// NewNotifier creates a new Slack notifier with the given dependencies
func NewNotifier(client SlackClient, repository ThreadRepository, debouncer Debouncer) *Notifier {
	return &Notifier{
		client:     client,
		repository: repository,
		debouncer:  debouncer,
		pending:    make(map[string]*pendingFlush),
		reactions:  make(map[string]string),
		retryWait:  5 * time.Second,
	}
}

// Notify sends a notification to Slack (debounced)
func (n *Notifier) Notify(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) error {
	// Silently skip if no Slack config
	if target.SlackChannel == "" || target.SlackBotToken == "" {
		return nil
	}

	key := fmt.Sprintf("%s#%d", target.SlackChannel, event.IssueNumber)

	// Buffer event in two-lane pending: comments queue, status overwrites
	n.mu.Lock()
	p := n.pending[key]
	if p == nil {
		p = &pendingFlush{}
		n.pending[key] = p
	}
	if event.IsComment() {
		p.comments = append(p.comments, &event)
	} else {
		p.status = &event
	}
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

	// All events go through debouncer — comments no longer bypass
	n.debouncer.Debounce(key, 2*time.Second, func() {
		n.flush(ctx, key, target)
	})

	return nil
}

// flush sends the buffered notification (status-first, then comments)
func (n *Notifier) flush(ctx context.Context, key string, target targets.TargetConfig) {
	n.mu.Lock()
	p := n.pending[key]
	delete(n.pending, key)
	n.mu.Unlock()

	if p == nil {
		return
	}

	var thread *SlackThread

	// Phase 1: process status event (creates/finds thread)
	if p.status != nil {
		event := p.status

		// Find existing thread for this GitHub number
		var err error
		thread, err = n.repository.FindThreadByNumber(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)
		if err != nil {
			thread = nil
		}

		// Check linked issue before retry sleep — this is the common path for
		// agent-created PRs that reference a parent issue (e.g. "Fixes #16").
		if thread == nil && event.LinkedIssueNumber > 0 && event.LinkedIssueNumber != event.IssueNumber {
			thread, err = n.repository.FindThreadByNumber(ctx, event.RepoOwner, event.RepoName, event.LinkedIssueNumber)
			if err != nil {
				thread = nil
			}
			if thread != nil {
				// Found the linked issue's thread — create a new mapping for this PR
				// so future events (comments, merges) find the thread directly.
				if event.IsPR() {
					prThread := &SlackThread{
						ID:            uuid.New().String(),
						RepoOwner:     thread.RepoOwner,
						RepoName:      thread.RepoName,
						GithubPRID:    event.IssueNumber,
						SlackChannel:  thread.SlackChannel,
						SlackThreadTs: thread.SlackThreadTs,
						SlackParentTs: thread.SlackParentTs,
						ThreadType:    "pull_request",
					}
					if err := n.repository.SaveThread(ctx, prThread); err != nil {
						slog.Warn("failed to save PR thread mapping", "error", err, "pr", event.IssueNumber)
					}
				}
				slog.Info("linked PR to existing issue thread",
					"pr", event.IssueNumber,
					"linkedIssue", event.LinkedIssueNumber,
					"repo", event.RepoOwner+"/"+event.RepoName,
				)
			}
		}

		// Retry once for new issue/PR events when no thread found yet: the agent
		// may have created the issue via GitHub API and the webhook arrived before
		// the handler saved the thread mapping.
		if thread == nil && (event.Type == slackfacade.EventIssueOpened || event.Type == slackfacade.EventPROpened) {
			time.Sleep(n.retryWait)
			thread, err = n.repository.FindThreadByNumber(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)
			if err != nil {
				thread = nil
			}
		}

		newThread := false
		if thread == nil {
			newThread = true
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
				ThreadType:    event.ThreadType(),
			}

			if err := n.repository.SaveThread(ctx, thread); err != nil {
				slog.Warn("failed to save slack thread", "error", err)
			}

			n.mu.Lock()
			n.reactions[parentTs] = event.Emoji
			n.mu.Unlock()
		} else if event.IsPR() && thread.GithubPRID == 0 && thread.GithubIssueID == event.IssueNumber {
			// Same GitHub number was first seen as an issue, now arriving as a PR
			// (PRs are issues in GitHub and share the numbering space).
			thread.GithubPRID = event.IssueNumber
			thread.ThreadType = "pull_request"
			if err := n.repository.SaveThread(ctx, thread); err != nil {
				slog.Warn("failed to update thread type", "error", err)
			}
		}

		// Post status update to thread (only if not the first message creating the thread)
		if !newThread {
			updateMsg := formatEventMessage(*event)
			if err := n.client.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, updateMsg); err != nil {
				slog.Warn("failed to post to slack thread",
					"error", err,
					"channel", thread.SlackChannel,
					"thread", thread.SlackThreadTs,
				)
			}
		}
	}

	// Phase 2: process comments
	if len(p.comments) > 0 {
		// If thread wasn't resolved in phase 1, look it up
		if thread == nil {
			ref := p.comments[0]
			var err error
			thread, err = n.repository.FindThreadByNumber(ctx, ref.RepoOwner, ref.RepoName, ref.IssueNumber)
			if err != nil {
				thread = nil
			}
			// Retry once — covers the case where another handler is creating
			// the thread concurrently (e.g. agent created the issue via API).
			if thread == nil {
				time.Sleep(n.retryWait)
				thread, err = n.repository.FindThreadByNumber(ctx, ref.RepoOwner, ref.RepoName, ref.IssueNumber)
				if err != nil {
					thread = nil
				}
			}
		}

		if thread == nil {
			slog.Info("skipping comment notifications: no thread found",
				"repo", p.comments[0].RepoOwner+"/"+p.comments[0].RepoName,
				"number", p.comments[0].IssueNumber,
				"count", len(p.comments),
			)
			return
		}

		for _, comment := range p.comments {
			updateMsg := formatEventMessage(*comment)
			if err := n.client.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, updateMsg); err != nil {
				slog.Warn("failed to post comment to slack thread",
					"error", err,
					"channel", thread.SlackChannel,
					"thread", thread.SlackThreadTs,
				)
			}
		}
	}
}

// formatParentMessage creates the initial thread message
func formatParentMessage(event slackfacade.NotificationEvent) MessageBlock {
	lines := []string{
		fmt.Sprintf("*%s #%d* — %s", event.IssueOrPR(), event.IssueNumber, event.Title),
		fmt.Sprintf("by @%s", event.Author),
	}

	// Add body/description if present (sanitized + truncated in code block)
	if event.Body != "" {
		bodyPreview := truncate(sanitizeForCodeBlock(event.Body), 300)
		lines = append(lines, "", fmt.Sprintf("```\n%s\n```", bodyPreview))
	}

	// Add link to GitHub
	lines = append(lines, "", fmt.Sprintf("<%s|View on GitHub>", event.GitHubURL()))

	return MessageBlock{Text: strings.Join(lines, "\n")}
}

const threadSeparator = "─────"

// formatEventMessage formats an update message for a thread
func formatEventMessage(event slackfacade.NotificationEvent) MessageBlock {
	var text string

	switch event.Type {
	case slackfacade.EventPRReady:
		lines := []string{
			threadSeparator,
			"*Preview ready*",
			fmt.Sprintf("<%s|Open Preview>", event.PreviewURL),
		}
		if event.LogsURL != "" {
			lines = append(lines, fmt.Sprintf("<%s|View Logs>", event.LogsURL))
		}
		if event.UserNote != "" {
			lines = append(lines, fmt.Sprintf("> *Note:* %s", event.UserNote))
		}
		text = strings.Join(lines, "\n")

	case slackfacade.EventPRFailed:
		text = fmt.Sprintf("%s\n*Preview failed*\n> Stage: `%s`", threadSeparator, event.Status)

	case slackfacade.EventPROpened, slackfacade.EventIssueOpened:
		text = fmt.Sprintf("%s\nOpened by @%s", threadSeparator, event.Author)

	case slackfacade.EventCommentAdded:
		preview := truncate(sanitizeForCodeBlock(event.Body), 300)
		url := event.CommentURL()
		if url != "" {
			text = fmt.Sprintf("%s\n*@%s* commented:\n```\n%s\n```\n<%s|View on GitHub>", threadSeparator, event.Author, preview, url)
		} else {
			text = fmt.Sprintf("%s\n*@%s* commented:\n```\n%s\n```", threadSeparator, event.Author, preview)
		}

	case slackfacade.EventPRMerged:
		text = fmt.Sprintf("%s\n*Merged* — Preview will be removed shortly", threadSeparator)

	case slackfacade.EventIssueClosed:
		text = fmt.Sprintf("%s\n*Closed*", threadSeparator)

	case slackfacade.EventPRClosed:
		text = fmt.Sprintf("%s\n*Closed* — Preview removed", threadSeparator)

	default:
		text = fmt.Sprintf("%s\nUpdate: %s", threadSeparator, event.Type)
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

// Regex patterns for sanitizeForCodeBlock (compiled once at package level)
var (
	htmlTagRe       = regexp.MustCompile(`<[^>]*>`)
	mdImageRe       = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	mdLinkRe        = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	headingMarkerRe = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	boldRe          = regexp.MustCompile(`\*\*(.+?)\*\*`)
	italicRe        = regexp.MustCompile(`(?:^|[^\\])_(.+?)_`)
	tripleTickRe    = regexp.MustCompile("```[a-zA-Z]*")
	excessNewlineRe = regexp.MustCompile(`\n{3,}`)
)

// sanitizeForCodeBlock transforms raw GitHub markdown into plain text
// suitable for display inside a Slack code block.
func sanitizeForCodeBlock(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = mdImageRe.ReplaceAllString(s, "$1")
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = headingMarkerRe.ReplaceAllString(s, "")
	s = boldRe.ReplaceAllString(s, "$1")
	s = italicRe.ReplaceAllString(s, "$1")
	s = tripleTickRe.ReplaceAllString(s, "")
	s = excessNewlineRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
