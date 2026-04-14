package domain

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// phaseTrackingRepository extends mockRepository to track phase updates
type phaseTrackingRepository struct {
	*mockRepository
	mu          sync.Mutex
	phaseCalls  []phaseCall
	notifyCalls int
	feedbackSet []bool
}

type phaseCall struct {
	ThreadTs string
	Phase    WorkstreamPhase
}

func newPhaseTrackingRepository() *phaseTrackingRepository {
	return &phaseTrackingRepository{
		mockRepository: newMockRepository(),
	}
}

func (p *phaseTrackingRepository) UpdateWorkstreamPhase(_ context.Context, threadTs string, phase WorkstreamPhase) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phaseCalls = append(p.phaseCalls, phaseCall{ThreadTs: threadTs, Phase: phase})
	// Also update the mock data
	for _, thread := range p.threads {
		if thread.SlackThreadTs == threadTs {
			thread.WorkstreamPhase = phase
		}
	}
	return nil
}

func (p *phaseTrackingRepository) SetPreviewNotified(_ context.Context, threadTs string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notifyCalls++
	return nil
}

func (p *phaseTrackingRepository) SetFeedbackRelayed(_ context.Context, threadTs string, relayed bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.feedbackSet = append(p.feedbackSet, relayed)
	return nil
}

func (p *phaseTrackingRepository) getPhaseCalls() []phaseCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.phaseCalls
}

func (p *phaseTrackingRepository) getNotifyCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.notifyCalls
}

func TestNotifier_PreviewReady_TransitionsInProgressToReview(t *testing.T) {
	client := &mockClient{}
	repo := newPhaseTrackingRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp-test",
	}

	// Pre-seed a thread in in-progress phase
	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "test-ws-1",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubIssueID:   10,
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

	calls := repo.getPhaseCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase transition to review after preview_ready")
	}
	if calls[0].Phase != PhaseReview {
		t.Errorf("Expected phase %q, got %q", PhaseReview, calls[0].Phase)
	}
}

func TestNotifier_PreviewReady_TransitionsRevisionToReview(t *testing.T) {
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
		ID:              "test-ws-2",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubIssueID:   10,
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

	calls := repo.getPhaseCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase transition to review after preview_ready in revision")
	}
	if calls[0].Phase != PhaseReview {
		t.Errorf("Expected phase %q, got %q", PhaseReview, calls[0].Phase)
	}
	// Also verify feedback_relayed was reset
	if len(repo.feedbackSet) == 0 || repo.feedbackSet[len(repo.feedbackSet)-1] != false {
		t.Error("Expected feedback_relayed to be reset to false on new preview")
	}
}

func TestNotifier_PROpened_TransitionsOpenToInProgress(t *testing.T) {
	client := &mockClient{}
	repo := newPhaseTrackingRepository()
	debouncer := newMockDebouncer()
	notifier := NewNotifier(client, repo, debouncer, &mockAssembler{})

	target := targets.TargetConfig{
		SlackChannel:  "#test",
		SlackBotToken: "xoxb-test",
		GitHubPAT:     "ghp-test",
	}

	// Pre-seed a thread in open phase (issue exists, no PR yet)
	repo.SaveThread(context.Background(), &SlackThread{
		ID:              "test-ws-3",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubIssueID:   10,
		SlackChannel:    "#test",
		SlackThreadTs:   "333.333",
		ThreadType:      "issue",
		WorkstreamPhase: PhaseOpen,
	})

	event := slackfacade.NotificationEvent{
		Type:              slackfacade.EventPROpened,
		RepoOwner:         "org",
		RepoName:          "repo",
		IssueNumber:       15,
		LinkedIssueNumber: 10,
		Author:            "dev-bot",
	}

	notifier.Notify(context.Background(), event, target)
	debouncer.executeAll()

	calls := repo.getPhaseCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase transition to in-progress after PR opened")
	}
	found := false
	for _, c := range calls {
		if c.Phase == PhaseInProgress {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected phase transition to in-progress, got: %v", calls)
	}
}

func TestNotifier_PRMerged_TransitionsToDone(t *testing.T) {
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
		ID:              "test-ws-4",
		RepoOwner:       "org",
		RepoName:        "repo",
		GithubPRID:      15,
		SlackChannel:    "#test",
		SlackThreadTs:   "444.444",
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

	calls := repo.getPhaseCalls()
	found := false
	for _, c := range calls {
		if c.Phase == PhaseDone {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected phase transition to done after PR merged, got: %v", calls)
	}
}

// Ensure unused import is satisfied
var _ = fmt.Sprintf
