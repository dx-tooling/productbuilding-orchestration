package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

func TestNotifier_PreviewReady_InProgressPhase_ProducesReviewPrompt(t *testing.T) {
	client := &mockClient{}
	repo := newPhaseTrackingRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp-test",
	}

	// Thread in in-progress phase (first preview about to go live)
	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "proactive-1",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubPRID:      15,
		SlackChannel:    "#test",
		SlackThreadTs:   "111.111",
		ThreadType:      "pull_request",
		WorkstreamPhase: PhaseInProgress,
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "org",
		RepoName:    "repo",
		IssueNumber: 15,
		PreviewURL:  "https://preview-15.example.com",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	// Check that the posted message uses PM-style framing
	found := false
	for _, msg := range client.postedMessages {
		if msg.Thread != "" && strings.Contains(msg.Text, "what you think") {
			found = true
			break
		}
	}
	if !found {
		var texts []string
		for _, msg := range client.postedMessages {
			texts = append(texts, msg.Text)
		}
		t.Errorf("Expected PM-style review prompt, got messages: %v", texts)
	}
}

func TestNotifier_PreviewReady_RevisionPhase_ProducesFeedbackFollowup(t *testing.T) {
	client := &mockClient{}
	repo := newPhaseTrackingRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp-test",
	}

	// Thread in revision phase (user gave feedback, dev pushed fix)
	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "proactive-2",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubPRID:      15,
		SlackChannel:    "#test",
		SlackThreadTs:   "222.222",
		ThreadType:      "pull_request",
		WorkstreamPhase: PhaseRevision,
		FeedbackRelayed: true,
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRReady,
		RepoOwner:   "org",
		RepoName:    "repo",
		IssueNumber: 15,
		PreviewURL:  "https://preview-15.example.com",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	found := false
	for _, msg := range client.postedMessages {
		if msg.Thread != "" && strings.Contains(strings.ToLower(msg.Text), "feedback") {
			found = true
			break
		}
	}
	if !found {
		var texts []string
		for _, msg := range client.postedMessages {
			texts = append(texts, msg.Text)
		}
		t.Errorf("Expected feedback follow-up message, got: %v", texts)
	}
}

func TestNotifier_PRMerged_ReviewPhase_ProducesLiveConfirmation(t *testing.T) {
	client := &mockClient{}
	repo := newPhaseTrackingRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp-test",
	}

	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "proactive-3",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubPRID:      15,
		SlackChannel:    "#test",
		SlackThreadTs:   "333.333",
		ThreadType:      "pull_request",
		WorkstreamPhase: PhaseReview,
	})

	event := slackfacade.NotificationEvent{
		Type:        slackfacade.EventPRMerged,
		RepoOwner:   "org",
		RepoName:    "repo",
		IssueNumber: 15,
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	found := false
	for _, msg := range client.postedMessages {
		if msg.Thread != "" && strings.Contains(strings.ToLower(msg.Text), "live") {
			found = true
			break
		}
	}
	if !found {
		var texts []string
		for _, msg := range client.postedMessages {
			texts = append(texts, msg.Text)
		}
		t.Errorf("Expected 'live' confirmation, got: %v", texts)
	}
}
