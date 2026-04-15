package domain

import (
	"context"
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

	// Preview events come from the preview service (not webhooks), so the
	// notifier posts the template message directly.
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message for EventPRReady, got %d: %+v", len(client.postedMessages), client.postedMessages)
	}

	// Phase transition must still happen: InProgress → Review
	phaseCalls := repo.getPhaseCalls()
	if len(phaseCalls) == 0 {
		t.Fatal("Expected phase transition to PhaseReview, got none")
	}
	if phaseCalls[0].Phase != PhaseReview {
		t.Errorf("Expected phase = %s, got %s", PhaseReview, phaseCalls[0].Phase)
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

	// Preview events come from the preview service — notifier posts template.
	if len(client.postedMessages) != 1 {
		t.Fatalf("Expected 1 message for EventPRReady (revision phase), got %d: %+v", len(client.postedMessages), client.postedMessages)
	}

	// Phase transition must still happen: Revision → Review
	phaseCalls := repo.getPhaseCalls()
	if len(phaseCalls) == 0 {
		t.Fatal("Expected phase transition to PhaseReview, got none")
	}
	if phaseCalls[0].Phase != PhaseReview {
		t.Errorf("Expected phase = %s, got %s", PhaseReview, phaseCalls[0].Phase)
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

	// EventPRMerged is now delegated to the agent invoker — no template message should be posted.
	if len(client.postedMessages) != 0 {
		t.Errorf("Expected no messages (agent handles EventPRMerged), got: %+v", client.postedMessages)
	}

	// Phase transition must still happen: Review → Done
	phaseCalls := repo.getPhaseCalls()
	if len(phaseCalls) == 0 {
		t.Fatal("Expected phase transition to PhaseDone, got none")
	}
	if phaseCalls[0].Phase != PhaseDone {
		t.Errorf("Expected phase = %s, got %s", PhaseDone, phaseCalls[0].Phase)
	}
}
