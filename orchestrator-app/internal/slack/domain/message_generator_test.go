package domain

import (
	"strings"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

func TestMessageGenerator_ParentMessage_Issue(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventIssueOpened,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 42,
		Title:       "Add dark mode",
		Body:        "Please add dark mode.",
		Author:      "alice",
	}
	snap := &featurecontext.FeatureSnapshot{
		Issue: &featurecontext.IssueState{Number: 42, Title: "Add dark mode", Body: "Please add dark mode.", State: "open"},
	}

	msg := g.ParentMessage(event, snap)

	if !strings.Contains(msg.Text, "#42") {
		t.Errorf("Expected #42 in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "alice") {
		t.Errorf("Expected alice in message, got: %s", msg.Text)
	}
	// Body should be in blockquote, not code block
	if !strings.Contains(msg.Text, "> ") {
		t.Errorf("Expected blockquote in message, got: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "```") {
		t.Errorf("Should not use code block, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "GitHub") {
		t.Errorf("Expected GitHub link in message, got: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "─────") {
		t.Errorf("Should not have separator, got: %s", msg.Text)
	}
}

func TestMessageGenerator_ParentMessage_PR(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Title:       "Dark mode PR",
		Author:      "alice",
	}
	snap := &featurecontext.FeatureSnapshot{
		PR: &featurecontext.PRState{Number: 10, Title: "Dark mode PR", Author: "alice", Additions: 50, Deletions: 10, URL: "https://github.com/acme/widgets/pull/10"},
	}

	msg := g.ParentMessage(event, snap)

	if !strings.Contains(msg.Text, "pull request") {
		t.Errorf("Expected 'pull request' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "alice") {
		t.Errorf("Expected alice in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "60 lines") {
		t.Errorf("Expected line count (50+10=60) in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PROpened(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Author:      "alice",
	}
	snap := &featurecontext.FeatureSnapshot{
		PR: &featurecontext.PRState{Number: 10, Author: "alice", Additions: 50, Deletions: 10, URL: "https://github.com/acme/widgets/pull/10"},
	}

	msg := g.EventMessage(event, snap)

	if !strings.Contains(msg.Text, "@alice opened a pull request") {
		t.Errorf("Expected '@alice opened a pull request' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "60 lines") {
		t.Errorf("Expected line count in message, got: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "─────") {
		t.Errorf("Should not have separator, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PreviewReady(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview.example.com",
		LogsURL:    "https://preview.example.com/logs",
	}

	msg := g.EventMessage(event, nil)

	if !strings.Contains(msg.Text, "preview is live") {
		t.Errorf("Expected 'preview is live' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "preview.example.com") {
		t.Errorf("Expected preview URL in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "Logs") {
		t.Errorf("Expected logs link in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PreviewReady_WithUserNote(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview.example.com",
		LogsURL:    "https://preview.example.com/logs",
		UserNote:   "Use test@example.com",
	}

	msg := g.EventMessage(event, nil)

	if !strings.Contains(msg.Text, "> *Note:*") {
		t.Errorf("Expected '> *Note:*' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "test@example.com") {
		t.Errorf("Expected user note text in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PreviewFailed(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:    slackfacade.EventPRFailed,
		Status:  "build: exit 1",
		LogsURL: "https://preview.example.com/logs",
	}

	msg := g.EventMessage(event, nil)

	if !strings.Contains(msg.Text, "failed during") {
		t.Errorf("Expected 'failed during' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "build") {
		t.Errorf("Expected build stage in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "logs") {
		t.Errorf("Expected logs link in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "ask me to investigate") {
		t.Errorf("Expected 'ask me to investigate' in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_CommentAdded(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Author:      "bob",
		Body:        "Looks good!",
		CommentID:   123,
	}

	msg := g.EventMessage(event, nil)

	if !strings.Contains(msg.Text, "@bob commented on GitHub:") {
		t.Errorf("Expected '@bob commented on GitHub:' in message, got: %s", msg.Text)
	}
	// Body should be in blockquote, not code block
	if !strings.Contains(msg.Text, "> Looks good!") {
		t.Errorf("Expected body in blockquote, got: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "```") {
		t.Errorf("Should not use code block, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "View comment") {
		t.Errorf("Expected 'View comment' link, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PRMerged_WithCI(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type: slackfacade.EventPRMerged,
	}
	snap := &featurecontext.FeatureSnapshot{
		CIStatus: featurecontext.CIPassing,
	}

	msg := g.EventMessage(event, snap)

	if !strings.Contains(msg.Text, "merged") {
		t.Errorf("Expected 'merged' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "CI was passing") {
		t.Errorf("Expected 'CI was passing' in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_PRMerged_NoCIInfo(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type: slackfacade.EventPRMerged,
	}
	snap := &featurecontext.FeatureSnapshot{
		CIStatus: featurecontext.CIUnknown,
	}

	msg := g.EventMessage(event, snap)

	if !strings.Contains(msg.Text, "merged") {
		t.Errorf("Expected 'merged' in message, got: %s", msg.Text)
	}
	if strings.Contains(msg.Text, "CI") {
		t.Errorf("Should not mention CI when unknown, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_IssueClosed_ReturnsEmpty(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type: slackfacade.EventIssueClosed,
	}
	snap := &featurecontext.FeatureSnapshot{
		PR: &featurecontext.PRState{Number: 52, Merged: true},
	}

	msg := g.EventMessage(event, snap)

	if msg.Text != "" {
		t.Errorf("Expected empty message for IssueClosed (removed), got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_CIFailed(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:           slackfacade.EventCIFailed,
		CheckRunName:   "build",
		FailureSummary: "expected 3 got 5",
		WorkflowURL:    "https://github.com/acme/widgets/runs/100",
	}

	msg := g.EventMessage(event, nil)

	if !strings.Contains(msg.Text, "CI failed") {
		t.Errorf("Expected 'CI failed' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "build") {
		t.Errorf("Expected check name in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "> expected 3 got 5") {
		t.Errorf("Expected failure summary in blockquote, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "View run") {
		t.Errorf("Expected 'View run' link, got: %s", msg.Text)
	}
}

func TestMessageGenerator_EventMessage_CIPassed_ReturnsEmpty(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type: slackfacade.EventCIPassed,
	}

	msg := g.EventMessage(event, nil)

	if msg.Text != "" {
		t.Errorf("Expected empty message for CIPassed (removed), got: %s", msg.Text)
	}
}

func TestMessageGenerator_RemovedEventCases_ReturnEmpty(t *testing.T) {
	g := NewMessageGenerator()
	removedEvents := []slackfacade.EventType{
		slackfacade.EventIssueOpened,
		slackfacade.EventIssueReopened,
		slackfacade.EventIssueClosed,
		slackfacade.EventPRClosed,
		slackfacade.EventCIPassed,
	}

	for _, eventType := range removedEvents {
		event := slackfacade.NotificationEvent{
			Type:      eventType,
			Author:    "alice",
			RepoOwner: "acme",
			RepoName:  "widgets",
		}
		msg := g.EventMessage(event, nil)
		if msg.Text != "" {
			t.Errorf("EventMessage(%s) = %q, want empty string (case removed)", eventType, msg.Text)
		}
	}
}

func TestMessageGenerator_PROpened_PhaseOpen_ReturnsEmpty(t *testing.T) {
	g := NewMessageGenerator()
	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		RepoOwner:   "acme",
		RepoName:    "widgets",
		IssueNumber: 10,
		Author:      "alice",
	}

	msg := g.EventMessage(event, nil, PhaseOpen)
	if msg.Text != "" {
		t.Errorf("EventMessage(PROpened, PhaseOpen) = %q, want empty string", msg.Text)
	}
}

func TestMessageGenerator_NilSnapshot_Fallback(t *testing.T) {
	g := NewMessageGenerator()

	// Events that should still produce non-empty messages
	activeEvents := []slackfacade.NotificationEvent{
		{Type: slackfacade.EventPROpened, IssueNumber: 2, Title: "Test PR", Author: "bob", RepoOwner: "a", RepoName: "b"},
		{Type: slackfacade.EventPRMerged},
		{Type: slackfacade.EventCommentAdded, Author: "carol", Body: "hi"},
		{Type: slackfacade.EventPRReady, PreviewURL: "https://example.com"},
		{Type: slackfacade.EventPRFailed, Status: "build"},
	}

	for _, event := range activeEvents {
		// Should not panic with nil snapshot
		parent := g.ParentMessage(event, nil)
		if parent.Text == "" {
			t.Errorf("ParentMessage should produce non-empty text for %s", event.Type)
		}

		reply := g.EventMessage(event, nil)
		if reply.Text == "" {
			t.Errorf("EventMessage should produce non-empty text for %s", event.Type)
		}
	}

	// Removed events should return empty EventMessage (but ParentMessage may still work)
	removedEvents := []slackfacade.NotificationEvent{
		{Type: slackfacade.EventIssueOpened, IssueNumber: 1, Title: "Test", Author: "alice", RepoOwner: "a", RepoName: "b"},
		{Type: slackfacade.EventIssueClosed},
		{Type: slackfacade.EventCIPassed},
	}

	for _, event := range removedEvents {
		// Should not panic
		g.ParentMessage(event, nil)

		reply := g.EventMessage(event, nil)
		if reply.Text != "" {
			t.Errorf("EventMessage should return empty for removed event %s, got %q", event.Type, reply.Text)
		}
	}
}
