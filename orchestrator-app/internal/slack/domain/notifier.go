package domain

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
	"github.com/google/uuid"
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
	UpdateWorkstreamPhase(ctx context.Context, threadTs string, phase WorkstreamPhase) error
	SetPreviewNotified(ctx context.Context, threadTs string) error
	SetFeedbackRelayed(ctx context.Context, threadTs string, relayed bool) error
}

// Debouncer batches rapid events
type Debouncer interface {
	Debounce(key string, wait time.Duration, fn func())
}

// FeatureContextAssembler fetches aggregated feature context for enriching notifications.
type FeatureContextAssembler interface {
	ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*featurecontext.FeatureSnapshot, error)
	ForIssue(ctx context.Context, owner, repo, pat string, number int) (*featurecontext.FeatureSnapshot, error)
}

// pendingFlush separates status events (deduped by overwrite) from comments (queued).
type pendingFlush struct {
	status   *slackfacade.NotificationEvent   // latest lifecycle/status event (overwrite)
	comments []*slackfacade.NotificationEvent // all comments in arrival order (append)
}

// NotifierOption configures optional Notifier dependencies.
type NotifierOption func(*Notifier)

// WithEventNarrator enables LLM-backed conversational narration for preview events.
// When set, EventPRReady and EventPRFailed are narrated by the agent instead of
// using template messages. Falls back to the template if the agent call fails.
func WithEventNarrator(narrator EventAgentRunner) NotifierOption {
	return func(n *Notifier) { n.narrator = narrator }
}

// SetEventNarrator sets the event narrator after construction. Useful when the
// narrator depends on components built after the notifier (e.g., the LLM client).
func (n *Notifier) SetEventNarrator(narrator EventAgentRunner) {
	n.narrator = narrator
}

// Notifier sends notifications to Slack with debouncing and emoji reactions
type Notifier struct {
	client     SlackClient
	repository ThreadRepository
	debouncer  Debouncer
	assembler  FeatureContextAssembler  // enriches notifications with feature context
	narrator   EventAgentRunner         // optional: LLM narrator for preview events
	pending    map[string]*pendingFlush // key: channel#issue -> two-lane buffer
	reactions  map[string]string        // threadTs -> current emoji
	retryWait  time.Duration            // wait before retry lookups (default 5s)
	mu         sync.Mutex
}

// NewNotifier creates a new Slack notifier with the given dependencies
func NewNotifier(client SlackClient, repository ThreadRepository, debouncer Debouncer, assembler FeatureContextAssembler, opts ...NotifierOption) *Notifier {
	n := &Notifier{
		client:     client,
		repository: repository,
		debouncer:  debouncer,
		assembler:  assembler,
		pending:    make(map[string]*pendingFlush),
		reactions:  make(map[string]string),
		retryWait:  5 * time.Second,
	}
	for _, opt := range opts {
		opt(n)
	}
	return n
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

	msgs := NewMessageGeneratorWithLanguage(target.LanguageOrDefault())

	var thread *SlackThread

	// Assemble feature context for enriching messages
	var snap *featurecontext.FeatureSnapshot
	if n.assembler != nil {
		// Use the first available event to determine what to assemble
		var refEvent *slackfacade.NotificationEvent
		if p.status != nil {
			refEvent = p.status
		} else if len(p.comments) > 0 {
			refEvent = p.comments[0]
		}
		if refEvent != nil {
			if refEvent.IsPR() {
				snap, _ = n.assembler.ForPR(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, refEvent.IssueNumber, refEvent.LinkedIssueNumber)
			} else {
				// Check if thread has a linked PR (e.g., issue closed after PR merged)
				if t, _ := n.repository.FindThreadByNumber(ctx, refEvent.RepoOwner, refEvent.RepoName, refEvent.IssueNumber); t != nil && t.GithubPRID > 0 {
					snap, _ = n.assembler.ForPR(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, t.GithubPRID, refEvent.IssueNumber)
				} else {
					snap, _ = n.assembler.ForIssue(ctx, refEvent.RepoOwner, refEvent.RepoName, target.GitHubPAT, refEvent.IssueNumber)
				}
			}
		}
	}

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

		// Only EventIssueOpened and EventPROpened should create new threads.
		// Other events (CI, preview, close, merge) are only meaningful as
		// replies in existing threads — skip them if no thread exists.
		if thread == nil && event.Type != slackfacade.EventIssueOpened && event.Type != slackfacade.EventPROpened {
			slog.Info("skipping notification: no existing thread for non-creation event",
				"event", event.Type,
				"repo", event.RepoOwner+"/"+event.RepoName,
				"number", event.IssueNumber,
			)
			p.status = nil
		}

		if p.status != nil {
			newThread := false
			if thread == nil {
				newThread = true
				parentMsg := msgs.ParentMessage(*event, snap)
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
			// Skip messages that are either bot self-narration or delegated to the agent invoker.
			skipMsg := shouldSkipMessage(event.Type)
			// Special case: EventPROpened in PhaseOpen is also skipped (agent will narrate)
			if event.Type == slackfacade.EventPROpened && thread.WorkstreamPhase == PhaseOpen {
				skipMsg = true
			}
			if !newThread && !skipMsg {
				posted := false

				// For preview events, try the LLM narrator first for a conversational message.
				if n.narrator != nil && isNarratableEvent(event.Type) {
					narratorText := n.tryNarrate(ctx, *event, thread, target)
					if narratorText != "" {
						if err := n.client.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, MessageBlock{Text: narratorText}); err != nil {
							slog.Warn("failed to post narrator response to slack thread", "error", err)
						} else {
							posted = true
						}
					}
				}

				// Fall back to template message if narrator unavailable or failed.
				if !posted {
					updateMsg := msgs.EventMessage(*event, snap, thread.WorkstreamPhase)
					if err := n.client.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, updateMsg); err != nil {
						slog.Warn("failed to post to slack thread",
							"error", err,
							"channel", thread.SlackChannel,
							"thread", thread.SlackThreadTs,
						)
					}
				}
			}

			// Update workstream phase based on event type
			n.updatePhaseForEvent(ctx, thread, event.Type)
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

		// Skip template messages when agent invoker handles the event type
		if !shouldSkipMessage(p.comments[0].Type) {
			for _, comment := range p.comments {
				updateMsg := msgs.EventMessage(*comment, snap, thread.WorkstreamPhase)
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
}

// updatePhaseForEvent transitions the workstream phase based on a GitHub event.
func (n *Notifier) updatePhaseForEvent(ctx context.Context, thread *SlackThread, eventType slackfacade.EventType) {
	if thread == nil {
		return
	}

	var newPhase WorkstreamPhase

	switch eventType {
	case slackfacade.EventPROpened:
		if thread.WorkstreamPhase == PhaseOpen || thread.WorkstreamPhase == "" {
			newPhase = PhaseInProgress
		}
	case slackfacade.EventPRReady:
		if thread.WorkstreamPhase == PhaseInProgress || thread.WorkstreamPhase == PhaseRevision {
			newPhase = PhaseReview
		}
	case slackfacade.EventPRMerged:
		newPhase = PhaseDone
	}

	if newPhase != "" {
		if err := n.repository.UpdateWorkstreamPhase(ctx, thread.SlackThreadTs, newPhase); err != nil {
			slog.Warn("failed to update workstream phase", "error", err, "phase", newPhase)
		}

		// On preview ready: set preview notified and reset feedback relayed
		if eventType == slackfacade.EventPRReady {
			n.repository.SetPreviewNotified(ctx, thread.SlackThreadTs)
			n.repository.SetFeedbackRelayed(ctx, thread.SlackThreadTs, false)
		}
	}
}

// shouldSkipMessage returns true for events where message generation
// is either unnecessary (bot self-narration) or delegated to the agent invoker.
func shouldSkipMessage(eventType slackfacade.EventType) bool {
	switch eventType {
	case slackfacade.EventIssueOpened: // reply only; parent message is still posted
		return true
	case slackfacade.EventIssueReopened:
		return true
	case slackfacade.EventIssueClosed: // usually bot-initiated; template removed
		return true
	case slackfacade.EventPRClosed: // usually bot-initiated; template removed
		return true
	case slackfacade.EventCIPassed:
		return true
	case slackfacade.EventCIFailed: // agent invoker handles this
		return true
	case slackfacade.EventPRMerged: // agent invoker handles this
		return true
	case slackfacade.EventCommentAdded: // agent invoker handles this
		return true
	default:
		return false
	}
}

// truncate limits text length with ellipsis
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// Regex patterns for sanitizeBody (compiled once at package level).
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

// isNarratableEvent returns true for event types that should be narrated by
// the LLM agent instead of using template messages. Currently: preview events
// that originate from the preview service (not from GitHub webhooks).
func isNarratableEvent(eventType slackfacade.EventType) bool {
	switch eventType {
	case slackfacade.EventPRReady, slackfacade.EventPRFailed:
		return true
	default:
		return false
	}
}

// tryNarrate invokes the LLM narrator for a preview event and returns the
// conversational text. Returns "" if the narrator fails or returns empty.
func (n *Notifier) tryNarrate(ctx context.Context, event slackfacade.NotificationEvent, thread *SlackThread, target targets.TargetConfig) string {
	req := EventRunRequest{
		ChannelID:       thread.SlackChannel,
		ThreadTs:        thread.SlackThreadTs,
		UserText:        formatSystemEvent(event),
		Target:          target,
		WorkstreamPhase: thread.WorkstreamPhase,
	}

	resp, err := n.narrator.RunForEvent(ctx, req)
	if err != nil {
		slog.Warn("narrator failed for preview event, falling back to template",
			"event", event.Type,
			"error", err,
		)
		return ""
	}
	return resp.Text
}
