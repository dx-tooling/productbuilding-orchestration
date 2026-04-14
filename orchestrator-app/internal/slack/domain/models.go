package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SlackThread represents a mapping between a GitHub issue/PR and a Slack thread
type SlackThread struct {
	ID                string
	RepoOwner         string
	RepoName          string
	GithubIssueID     int
	GithubPRID        int
	SlackChannel      string
	SlackThreadTs     string
	SlackParentTs     string
	ThreadType        string
	WorkstreamPhase   WorkstreamPhase
	PreviewNotifiedAt *time.Time
	FeedbackRelayed   bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// NewSlackThread creates a new SlackThread with validation
func NewSlackThread(repoOwner, repoName string, issueNumber, prNumber int, channel, threadTs string) (*SlackThread, error) {
	// Validation
	if repoOwner == "" {
		return nil, errors.New("repo owner cannot be empty")
	}
	if repoName == "" {
		return nil, errors.New("repo name cannot be empty")
	}
	if channel == "" {
		return nil, errors.New("slack channel cannot be empty")
	}
	if threadTs == "" {
		return nil, errors.New("slack thread timestamp cannot be empty")
	}

	// Must have exactly one of issueNumber or prNumber
	if issueNumber > 0 && prNumber > 0 {
		return nil, errors.New("cannot set both issue number and PR number")
	}
	if issueNumber == 0 && prNumber == 0 {
		return nil, errors.New("must set either issue number or PR number")
	}

	threadType := "issue"
	if prNumber > 0 {
		threadType = "pull_request"
	}

	return &SlackThread{
		ID:            uuid.New().String(),
		RepoOwner:     repoOwner,
		RepoName:      repoName,
		GithubIssueID: issueNumber,
		GithubPRID:    prNumber,
		SlackChannel:  channel,
		SlackThreadTs: threadTs,
		ThreadType:    threadType,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}, nil
}

// EventType represents the type of GitHub event being notified
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
	Type        EventType
	RepoOwner   string
	RepoName    string
	IssueNumber int
	Title       string
	Body        string
	URL         string
	Author      string
	Status      string
	PreviewURL  string
	LogsURL     string
	UserNote    string
	CommentID   int64
	ThreadTs    string // For emoji reactions
	Emoji       string // Emoji to add as reaction
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

// IssueOrPR returns the string "Issue" or "Pull Request" based on event type
func (e NotificationEvent) IssueOrPR() string {
	if e.IsPR() {
		return "Pull Request"
	}
	return "Issue"
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

// MessageBlock represents a Slack message with optional block formatting
type MessageBlock struct {
	Text   string
	Blocks []Block
}

// Block represents a Slack block element
type Block struct {
	Type string
	Text *TextBlock
}

// TextBlock represents text within a Slack block
type TextBlock struct {
	Type  string
	Text  string
	Emoji bool
}

// NewTextBlock creates a simple text block
func NewTextBlock(text string) MessageBlock {
	return MessageBlock{
		Text: text,
	}
}
