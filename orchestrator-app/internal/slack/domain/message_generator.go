package domain

import (
	"fmt"
	"strings"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// MessageGenerator produces conversational PM-style Slack messages using feature context.
type MessageGenerator struct{}

func NewMessageGenerator() *MessageGenerator {
	return &MessageGenerator{}
}

// ParentMessage creates the initial thread message for an issue or PR.
func (g *MessageGenerator) ParentMessage(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot) MessageBlock {
	if event.IsPR() {
		return g.parentMessagePR(event, snap)
	}
	return g.parentMessageIssue(event, snap)
}

func (g *MessageGenerator) parentMessageIssue(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot) MessageBlock {
	title := event.Title
	number := event.IssueNumber
	author := event.Author
	body := event.Body
	url := event.GitHubURL()

	if snap != nil && snap.Issue != nil {
		if snap.Issue.Title != "" {
			title = snap.Issue.Title
		}
		if snap.Issue.Body != "" {
			body = snap.Issue.Body
		}
	}

	lines := []string{
		fmt.Sprintf("*#%d %s*", number, title),
		fmt.Sprintf("Opened by @%s", author),
	}

	if body != "" {
		bodyPreview := truncate(sanitizeBody(body), 300)
		lines = append(lines, "", fmt.Sprintf("> %s", strings.ReplaceAll(bodyPreview, "\n", "\n> ")))
	}

	lines = append(lines, "", fmt.Sprintf("<%s|GitHub>", url))

	return MessageBlock{Text: strings.Join(lines, "\n")}
}

func (g *MessageGenerator) parentMessagePR(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot) MessageBlock {
	title := event.Title
	number := event.IssueNumber
	author := event.Author
	url := event.GitHubURL()

	var lineInfo string
	if snap != nil && snap.PR != nil {
		if snap.PR.Title != "" {
			title = snap.PR.Title
		}
		if snap.PR.Author != "" {
			author = snap.PR.Author
		}
		if snap.PR.URL != "" {
			url = snap.PR.URL
		}
		total := snap.PR.Additions + snap.PR.Deletions
		if total > 0 {
			lineInfo = fmt.Sprintf(", touching %d lines", total)
		}
	}

	lines := []string{
		fmt.Sprintf("*#%d %s*", number, title),
		fmt.Sprintf("@%s opened a pull request%s", author, lineInfo),
	}

	lines = append(lines, fmt.Sprintf("<%s|View PR>", url))

	return MessageBlock{Text: strings.Join(lines, "\n")}
}

// EventMessage creates a thread reply message for an event update.
// The phase parameter enables PM-style framing based on the workstream lifecycle.
func (g *MessageGenerator) EventMessage(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot, phase ...WorkstreamPhase) MessageBlock {
	var currentPhase WorkstreamPhase
	if len(phase) > 0 {
		currentPhase = phase[0]
	}

	var text string

	switch event.Type {
	case slackfacade.EventPROpened:
		text = g.eventPROpened(event, snap, currentPhase)
	case slackfacade.EventPRReady:
		text = g.eventPreviewReady(event, currentPhase)
	case slackfacade.EventPRFailed:
		text = g.eventPreviewFailed(event)
	case slackfacade.EventCommentAdded, slackfacade.EventCommentEdited:
		text = g.eventComment(event)
	case slackfacade.EventPRMerged:
		text = g.eventPRMerged(snap, currentPhase)
	case slackfacade.EventIssueClosed:
		text = g.eventIssueClosed(snap)
	case slackfacade.EventPRClosed:
		text = "This pull request has been closed. The preview will be torn down shortly."
	case slackfacade.EventIssueOpened:
		text = fmt.Sprintf("@%s opened this issue.", event.Author)
	case slackfacade.EventIssueReopened:
		text = "This issue has been reopened."
	case slackfacade.EventCIFailed:
		text = g.eventCIFailed(event)
	case slackfacade.EventCIPassed:
		text = g.eventCIPassed(event)
	default:
		text = fmt.Sprintf("Update: %s", event.Type)
	}

	return MessageBlock{Text: text}
}

func (g *MessageGenerator) eventPROpened(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot, phase WorkstreamPhase) string {
	// PM-style framing when phase is open (user's issue is being worked on)
	if phase == PhaseOpen {
		return "Work has started on your request. I'll let you know when there's something to look at."
	}

	author := event.Author
	url := event.GitHubURL()
	var lineInfo string

	if snap != nil && snap.PR != nil {
		if snap.PR.Author != "" {
			author = snap.PR.Author
		}
		if snap.PR.URL != "" {
			url = snap.PR.URL
		}
		total := snap.PR.Additions + snap.PR.Deletions
		if total > 0 {
			lineInfo = fmt.Sprintf(", touching %d lines", total)
		}
	}

	return fmt.Sprintf("@%s opened a pull request%s.\n<%s|View PR>", author, lineInfo, url)
}

func (g *MessageGenerator) eventPreviewReady(event slackfacade.NotificationEvent, phase WorkstreamPhase) string {
	var heading string
	switch phase {
	case PhaseRevision:
		heading = "Updated preview is live — this should address the feedback you gave."
	case PhaseInProgress, PhaseOpen:
		heading = "This is ready for you to try out. Let me know what you think."
	default:
		heading = "The preview is live — you can try it out here:"
	}

	lines := []string{heading}
	links := fmt.Sprintf("<%s|Open Preview>", event.PreviewURL)
	if event.LogsURL != "" {
		links += fmt.Sprintf("  |  <%s|Logs>", event.LogsURL)
	}
	lines = append(lines, links)
	if event.UserNote != "" {
		lines = append(lines, fmt.Sprintf("> *Note:* %s", event.UserNote))
	}
	return strings.Join(lines, "\n")
}

func (g *MessageGenerator) eventPreviewFailed(event slackfacade.NotificationEvent) string {
	stage := event.Status
	if stage == "" {
		stage = "unknown"
	}

	// Extract just the stage name from compound statuses like "build: exit 1"
	stageName := stage
	if idx := strings.Index(stage, ":"); idx > 0 {
		stageName = strings.TrimSpace(stage[:idx])
	}

	text := fmt.Sprintf("The preview failed during the %s step.", stageName)
	if event.LogsURL != "" {
		text += fmt.Sprintf(" Check the <%s|logs> for details, or ask me to investigate.", event.LogsURL)
	} else {
		text += " Ask me to investigate if you'd like."
	}
	return text
}

func (g *MessageGenerator) eventComment(event slackfacade.NotificationEvent) string {
	body := truncate(sanitizeBody(event.Body), 300)
	quoted := "> " + strings.ReplaceAll(body, "\n", "\n> ")

	lines := []string{
		fmt.Sprintf("@%s commented on GitHub:", event.Author),
		quoted,
	}

	url := event.CommentURL()
	if url != "" {
		lines = append(lines, fmt.Sprintf("<%s|View comment>", url))
	}

	return strings.Join(lines, "\n")
}

func (g *MessageGenerator) eventPRMerged(snap *featurecontext.FeatureSnapshot, phase WorkstreamPhase) string {
	// PM-style confirmation when the user has been in a review cycle
	if phase == PhaseReview || phase == PhaseRevision {
		return "This is live now. Let me know if anything looks off in production."
	}

	text := "This PR has been merged."
	if snap != nil && snap.CIStatus == featurecontext.CIPassing {
		text += " CI was passing on the final commit."
	}
	text += " The preview will be torn down shortly."
	return text
}

func (g *MessageGenerator) eventIssueClosed(snap *featurecontext.FeatureSnapshot) string {
	if snap != nil && snap.PR != nil && snap.PR.Merged {
		return fmt.Sprintf("This issue is now closed. It was addressed by PR #%d, which has been merged.", snap.PR.Number)
	}
	return "This issue has been closed."
}

func (g *MessageGenerator) eventCIFailed(event slackfacade.NotificationEvent) string {
	lines := []string{"CI failed on the latest push."}

	if event.CheckRunName != "" {
		lines[0] = fmt.Sprintf("CI failed on the latest push. The `%s` job failed:", event.CheckRunName)
	}

	if event.FailureSummary != "" {
		lines = append(lines, fmt.Sprintf("> %s", event.FailureSummary))
	}

	if event.WorkflowURL != "" {
		lines = append(lines, fmt.Sprintf("<%s|View run>", event.WorkflowURL))
	}

	return strings.Join(lines, "\n")
}

func (g *MessageGenerator) eventCIPassed(event slackfacade.NotificationEvent) string {
	return "CI checks are passing on the latest push."
}

// sanitizeBody transforms raw GitHub markdown into plain text suitable for Slack blockquotes.
func sanitizeBody(s string) string {
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
