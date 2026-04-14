package domain

import (
	"strings"
	"testing"

	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

func TestMessageGenerator_PreviewReady_InProgress_ReviewPrompt(t *testing.T) {
	gen := NewMessageGenerator()

	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview-52.example.com",
		UserNote:   "Sign in with test@example.com / test1234",
	}

	msg := gen.EventMessage(event, nil, PhaseInProgress)

	hasTryOut := strings.Contains(msg.Text, "try")
	hasWhatYouThink := strings.Contains(msg.Text, "what you think")
	if !hasTryOut {
		t.Errorf("Expected 'try' in message, got: %s", msg.Text)
	}
	if !hasWhatYouThink {
		t.Errorf("Expected 'what you think' in message, got: %s", msg.Text)
	}
	if !strings.Contains(msg.Text, "preview-52.example.com") {
		t.Errorf("Expected preview URL in message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_PreviewReady_Revision_FeedbackFollowup(t *testing.T) {
	gen := NewMessageGenerator()

	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview-52.example.com",
	}

	msg := gen.EventMessage(event, nil, PhaseRevision)

	if !strings.Contains(strings.ToLower(msg.Text), "feedback") || !strings.Contains(strings.ToLower(msg.Text), "updated") {
		t.Errorf("Expected feedback follow-up framing for revision preview, got: %s", msg.Text)
	}
}

func TestMessageGenerator_PreviewReady_NoPhase_DefaultMessage(t *testing.T) {
	gen := NewMessageGenerator()

	event := slackfacade.NotificationEvent{
		Type:       slackfacade.EventPRReady,
		PreviewURL: "https://preview-52.example.com",
	}

	msg := gen.EventMessage(event, nil, "")

	// Should still work with the standard message
	if !strings.Contains(msg.Text, "preview") {
		t.Errorf("Expected preview mention in default message, got: %s", msg.Text)
	}
}

func TestMessageGenerator_PROpened_Open_ReturnsEmpty(t *testing.T) {
	gen := NewMessageGenerator()

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPROpened,
		IssueNumber: 52,
		Author:      "dev-bot",
		RepoOwner:   "org",
		RepoName:    "repo",
	}

	msg := gen.EventMessage(event, nil, PhaseOpen)

	// PROpened in PhaseOpen is now suppressed (agent invoker handles the narration)
	if msg.Text != "" {
		t.Errorf("Expected empty message for PROpened in PhaseOpen (agent handles), got: %s", msg.Text)
	}
}

func TestMessageGenerator_PRMerged_ReviewPhase_LiveConfirmation(t *testing.T) {
	gen := NewMessageGenerator()

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		IssueNumber: 52,
		RepoOwner:   "org",
		RepoName:    "repo",
	}

	msg := gen.EventMessage(event, nil, PhaseReview)

	if !strings.Contains(strings.ToLower(msg.Text), "live") {
		t.Errorf("Expected 'live' confirmation for merged PR, got: %s", msg.Text)
	}
	if !strings.Contains(strings.ToLower(msg.Text), "looks off") || !strings.Contains(strings.ToLower(msg.Text), "production") {
		t.Errorf("Expected production check invitation, got: %s", msg.Text)
	}
}
