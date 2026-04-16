package domain

import (
	"fmt"
	"strings"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// MessageGenerator produces conversational PM-style Slack messages using feature context.
type MessageGenerator struct {
	language string
}

func NewMessageGenerator() *MessageGenerator {
	return &MessageGenerator{language: "en"}
}

func NewMessageGeneratorWithLanguage(lang string) *MessageGenerator {
	return &MessageGenerator{language: lang}
}

func (g *MessageGenerator) t(key MessageKey) string {
	return LocalizedMsg(g.language, key)
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
		fmt.Sprintf(g.t(msgOpenedBy), author),
	}

	if body != "" {
		bodyPreview := truncate(sanitizeBody(body), 300)
		lines = append(lines, "", fmt.Sprintf("> %s", strings.ReplaceAll(bodyPreview, "\n", "\n> ")))
	}

	lines = append(lines, "", fmt.Sprintf("<%s|%s>", url, g.t(msgGitHub)))

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
			lineInfo = fmt.Sprintf(g.t(msgTouching), total)
		}
	}

	lines := []string{
		fmt.Sprintf("*#%d %s*", number, title),
		fmt.Sprintf(g.t(msgOpenedPR), author, lineInfo),
	}

	lines = append(lines, fmt.Sprintf("<%s|%s>", url, g.t(msgViewPR)))

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
		// PhaseOpen means the agent will narrate; return empty to suppress template message
		if currentPhase == PhaseOpen {
			return MessageBlock{}
		}
		text = g.eventPROpened(event, snap, currentPhase)
	case slackfacade.EventPRReady:
		text = g.eventPreviewReady(event, currentPhase)
	case slackfacade.EventPRFailed:
		text = g.eventPreviewFailed(event)
	case slackfacade.EventCommentAdded, slackfacade.EventCommentEdited:
		text = g.eventComment(event)
	case slackfacade.EventPRMerged:
		text = g.eventPRMerged(snap, currentPhase)
	case slackfacade.EventCIFailed:
		text = g.eventCIFailed(event)
	// Removed cases: these events are either suppressed (bot self-narration) or
	// delegated to the agent invoker for natural-language narration.
	case slackfacade.EventIssueOpened, slackfacade.EventIssueReopened,
		slackfacade.EventIssueClosed, slackfacade.EventPRClosed, slackfacade.EventCIPassed:
		return MessageBlock{}
	default:
		text = fmt.Sprintf(g.t(msgUpdate), event.Type)
	}

	return MessageBlock{Text: text}
}

func (g *MessageGenerator) eventPROpened(event slackfacade.NotificationEvent, snap *featurecontext.FeatureSnapshot, phase WorkstreamPhase) string {
	// PM-style framing when phase is open (user's issue is being worked on)
	if phase == PhaseOpen {
		return g.t(msgWorkStarted)
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
			lineInfo = fmt.Sprintf(g.t(msgTouching), total)
		}
	}

	return fmt.Sprintf(g.t(msgOpenedPR), author, lineInfo) + fmt.Sprintf(".\n<%s|%s>", url, g.t(msgViewPR))
}

func (g *MessageGenerator) eventPreviewReady(event slackfacade.NotificationEvent, phase WorkstreamPhase) string {
	var heading string
	switch phase {
	case PhaseRevision:
		heading = g.t(msgPreviewRevision)
	case PhaseInProgress, PhaseOpen:
		heading = g.t(msgPreviewReview)
	default:
		heading = g.t(msgPreviewLive)
	}

	lines := []string{heading}
	links := fmt.Sprintf("<%s|%s>", event.PreviewURL, g.t(msgOpenPreview))
	if event.LogsURL != "" {
		links += fmt.Sprintf("  |  <%s|%s>", event.LogsURL, g.t(msgLogs))
	}
	lines = append(lines, links)
	if event.UserNote != "" {
		lines = append(lines, fmt.Sprintf("> *%s* %s", g.t(msgNote), event.UserNote))
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

	text := fmt.Sprintf(g.t(msgPreviewFailed), stageName)
	if event.LogsURL != "" {
		text += fmt.Sprintf(g.t(msgLogsForDetails), event.LogsURL)
	} else {
		text += g.t(msgAskInvestigate)
	}
	return text
}

func (g *MessageGenerator) eventComment(event slackfacade.NotificationEvent) string {
	body := truncate(sanitizeBody(event.Body), 300)
	quoted := "> " + strings.ReplaceAll(body, "\n", "\n> ")

	lines := []string{
		fmt.Sprintf(g.t(msgCommentedOnGH), event.Author),
		quoted,
	}

	url := event.CommentURL()
	if url != "" {
		lines = append(lines, fmt.Sprintf("<%s|%s>", url, g.t(msgViewComment)))
	}

	return strings.Join(lines, "\n")
}

func (g *MessageGenerator) eventPRMerged(snap *featurecontext.FeatureSnapshot, phase WorkstreamPhase) string {
	// PM-style confirmation when the user has been in a review cycle
	if phase == PhaseReview || phase == PhaseRevision {
		return g.t(msgPRMergedReview)
	}

	text := g.t(msgPRMerged)
	if snap != nil && snap.CIStatus == featurecontext.CIPassing {
		text += g.t(msgPRMergedCI)
	}
	text += g.t(msgPreviewTeardown)
	return text
}

func (g *MessageGenerator) eventCIFailed(event slackfacade.NotificationEvent) string {
	lines := []string{g.t(msgCIFailed)}

	if event.CheckRunName != "" {
		lines[0] = fmt.Sprintf(g.t(msgCIFailedJob), event.CheckRunName)
	}

	if event.FailureSummary != "" {
		lines = append(lines, fmt.Sprintf("> %s", event.FailureSummary))
	}

	if event.WorkflowURL != "" {
		lines = append(lines, fmt.Sprintf("<%s|%s>", event.WorkflowURL, g.t(msgViewRun)))
	}

	return strings.Join(lines, "\n")
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
