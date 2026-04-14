package domain

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// EventRunRequest is a simplified agent request for system event narration.
// This avoids an import cycle with the agent domain package.
type EventRunRequest struct {
	ChannelID       string
	ThreadTs        string
	UserText        string
	BotUserID       string
	Target          targets.TargetConfig
	WorkstreamPhase WorkstreamPhase
}

// EventRunResponse is a simplified agent response.
type EventRunResponse struct {
	Text string
}

// EventAgentRunner runs the agent for a system event.
type EventAgentRunner interface {
	RunForEvent(ctx context.Context, req EventRunRequest) (EventRunResponse, error)
}

// EventThreadFinder looks up a thread by issue/PR number.
type EventThreadFinder interface {
	FindThreadByNumber(ctx context.Context, repoOwner, repoName string, number int) (*SlackThread, error)
}

// SlackEventPoster posts messages to Slack threads.
type SlackEventPoster interface {
	PostToThread(ctx context.Context, botToken, channel, threadTs string, msg MessageBlock) error
}

// EventAgentInvoker invokes the LLM agent for system events that should be
// narrated conversationally, then posts the agent's response to the Slack thread.
type EventAgentInvoker struct {
	agent     EventAgentRunner
	threads   EventThreadFinder
	poster    SlackEventPoster
	retryWait time.Duration
}

// NewEventAgentInvoker creates a new EventAgentInvoker.
func NewEventAgentInvoker(agent EventAgentRunner, threads EventThreadFinder, poster SlackEventPoster, retryWait time.Duration) *EventAgentInvoker {
	return &EventAgentInvoker{
		agent:     agent,
		threads:   threads,
		poster:    poster,
		retryWait: retryWait,
	}
}

// InvokeForEvent looks up the Slack thread for the event, invokes the agent
// with a [system event] message, and posts the response to the thread.
// Designed to be called in a goroutine.
func (i *EventAgentInvoker) InvokeForEvent(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) {
	// Look up thread
	thread, _ := i.threads.FindThreadByNumber(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)

	// Retry once after retryWait if not found
	if thread == nil {
		time.Sleep(i.retryWait)
		thread, _ = i.threads.FindThreadByNumber(ctx, event.RepoOwner, event.RepoName, event.IssueNumber)
	}

	if thread == nil {
		slog.Info("event agent invoker: no thread found, skipping",
			"event", event.Type,
			"repo", event.RepoOwner+"/"+event.RepoName,
			"number", event.IssueNumber,
		)
		return
	}

	// Build the system event text for the agent
	userText := formatSystemEvent(event)

	req := EventRunRequest{
		ChannelID:       thread.SlackChannel,
		ThreadTs:        thread.SlackThreadTs,
		UserText:        userText,
		Target:          target,
		WorkstreamPhase: thread.WorkstreamPhase,
	}

	resp, err := i.agent.RunForEvent(ctx, req)
	if err != nil {
		slog.Warn("event agent invoker: agent run failed",
			"error", err,
			"event", event.Type,
			"number", event.IssueNumber,
		)
		return
	}

	if resp.Text == "" {
		return
	}

	if err := i.poster.PostToThread(ctx, target.SlackBotToken, thread.SlackChannel, thread.SlackThreadTs, MessageBlock{Text: resp.Text}); err != nil {
		slog.Warn("event agent invoker: failed to post response",
			"error", err,
			"thread", thread.SlackThreadTs,
		)
	}
}

// formatSystemEvent builds the [system event] text for the agent from the notification event.
func formatSystemEvent(event slackfacade.NotificationEvent) string {
	switch event.Type {
	case slackfacade.EventPRReady:
		text := fmt.Sprintf("[system event] The preview is now live at %s.", event.PreviewURL)
		if event.UserNote != "" {
			text += fmt.Sprintf(" Note: %s", event.UserNote)
		}
		return text

	case slackfacade.EventPRFailed:
		text := fmt.Sprintf("[system event] The preview deploy failed at stage %s.", event.Status)
		if event.LogsURL != "" {
			text += fmt.Sprintf(" Logs: %s", event.LogsURL)
		}
		return text

	case slackfacade.EventCIFailed:
		text := "[system event] CI failed on the latest push."
		if event.CheckRunName != "" {
			text += fmt.Sprintf(" Check: %s.", event.CheckRunName)
		}
		if event.FailureSummary != "" {
			text += fmt.Sprintf(" Summary: %s.", event.FailureSummary)
		}
		if event.WorkflowURL != "" {
			text += fmt.Sprintf(" Workflow: %s", event.WorkflowURL)
		}
		return text

	case slackfacade.EventPRMerged:
		return fmt.Sprintf("[system event] Pull request #%d has been merged.", event.IssueNumber)

	case slackfacade.EventCommentAdded:
		return fmt.Sprintf("[system event] @%s commented on GitHub issue #%d: %s", event.Author, event.IssueNumber, event.Body)

	case slackfacade.EventPROpened:
		text := fmt.Sprintf("[system event] %s opened pull request #%d", event.Author, event.IssueNumber)
		if event.Title != "" {
			text += fmt.Sprintf(": %s", event.Title)
		}
		return text

	default:
		return fmt.Sprintf("[system event] %s event for #%d.", event.Type, event.IssueNumber)
	}
}
