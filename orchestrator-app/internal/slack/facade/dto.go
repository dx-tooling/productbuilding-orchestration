package facade

import (
	"context"
	"fmt"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// EventType represents the type of GitHub event
type EventType string

const (
	EventIssueOpened   EventType = "issue_opened"
	EventIssueClosed   EventType = "issue_closed"
	EventIssueReopened EventType = "issue_reopened"
	EventPROpened      EventType = "pr_opened"
	EventPRReady       EventType = "pr_ready"
	EventPRFailed      EventType = "pr_failed"
	EventPRMerged      EventType = "pr_merged"
	EventPRClosed      EventType = "pr_closed"
	EventCommentAdded  EventType = "comment_added"
	EventCommentEdited EventType = "comment_edited"
)

// NotificationEvent represents a notification to be sent to Slack
type NotificationEvent struct {
	Type              EventType
	RepoOwner         string
	RepoName          string
	IssueNumber       int
	Title             string
	Body              string
	URL               string
	Author            string
	Status            string
	PreviewURL        string
	LogsURL           string
	UserNote          string
	CommentID         int64
	ThreadTs          string // For emoji reactions
	Emoji             string // Emoji to add as reaction
	LinkedIssueNumber int    // Issue number linked from PR body (e.g. "Fixes #16")
}

// IsPR returns true if the event is related to a pull request
func (e NotificationEvent) IsPR() bool {
	switch e.Type {
	case EventPROpened, EventPRReady, EventPRFailed, EventPRMerged, EventPRClosed:
		return true
	default:
		return false
	}
}

// IsComment returns true if the event is a comment (added or edited)
func (e NotificationEvent) IsComment() bool {
	return e.Type == EventCommentAdded || e.Type == EventCommentEdited
}

// IssueOrPR returns the string "Issue" or "Pull Request"
func (e NotificationEvent) IssueOrPR() string {
	if e.IsPR() {
		return "Pull Request"
	}
	return "Issue"
}

// ThreadType returns the database-friendly thread type: 'issue' or 'pull_request'
func (e NotificationEvent) ThreadType() string {
	if e.IsPR() {
		return "pull_request"
	}
	return "issue"
}

// GitHubURL returns the URL to view the issue/PR on GitHub
func (e NotificationEvent) GitHubURL() string {
	path := "issues"
	if e.IsPR() {
		path = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s/%d", e.RepoOwner, e.RepoName, path, e.IssueNumber)
}

// CommentURL returns the URL to view a specific comment on GitHub
func (e NotificationEvent) CommentURL() string {
	if e.CommentID == 0 {
		return ""
	}
	path := "issues"
	if e.IsPR() {
		path = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s/%d#issuecomment-%d", e.RepoOwner, e.RepoName, path, e.IssueNumber, e.CommentID)
}

// Notifier defines the interface for sending notifications
type Notifier interface {
	Notify(ctx context.Context, event NotificationEvent, target targets.TargetConfig) error
}
