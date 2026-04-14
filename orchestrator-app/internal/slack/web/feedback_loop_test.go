package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

func TestFeedbackLoop_ReviewPhase_DelegatedFeedback_TransitionsToRevision(t *testing.T) {
	// Scenario: preview is live (phase=review), user gives feedback, agent delegates it
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Got it, I've passed that to the developer.",
			SideEffects: agent.SideEffects{
				DelegatedIssues: []int{48},
			},
		},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID:   48,
			GithubPRID:      52,
			RepoOwner:       "example-org",
			RepoName:        "playground",
			SlackThreadTs:   "1111111111.111111",
			WorkstreamPhase: domain.PhaseReview,
		},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}
	phaseUpdater := &mockPhaseUpdater{}

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")
	h.SetPhaseUpdater(phaseUpdater)

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> the contrast on the sidebar is too low",
			"thread_ts": "1111111111.111111",
			"channel":   "C0PRODUCT",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	// Verify agent received the review phase
	lastReq := agentRunner.getLastReq()
	if lastReq.WorkstreamPhase != domain.PhaseReview {
		t.Errorf("Expected agent to receive phase %q, got %q", domain.PhaseReview, lastReq.WorkstreamPhase)
	}

	// Verify phase transitioned to revision
	calls := phaseUpdater.getCalls()
	foundRevision := false
	for _, c := range calls {
		if c.Phase == domain.PhaseRevision {
			foundRevision = true
			break
		}
	}
	if !foundRevision {
		t.Errorf("Expected phase transition to revision after feedback relay, got: %v", calls)
	}
}

func TestFeedbackLoop_ReviewPhase_NoSideEffects_StaysReview(t *testing.T) {
	// When user asks a question during review (not feedback), phase shouldn't change
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "The page loads slowly because of a large image on the hero section.",
			// No side effects — this was a research response, not feedback relay
		},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID:   48,
			GithubPRID:      52,
			RepoOwner:       "example-org",
			RepoName:        "playground",
			SlackThreadTs:   "1111111111.111111",
			WorkstreamPhase: domain.PhaseReview,
		},
	}
	slackClient := &mockSlackClient{
		userName:    "Alice",
		channelName: "productbuilding-playground",
	}
	registry := &mockTargetRegistry{
		channelConfig: defaultTarget(),
		channelFound:  true,
		botToken:      "xoxb-test",
	}
	phaseUpdater := &mockPhaseUpdater{}

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")
	h.SetPhaseUpdater(phaseUpdater)

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> why is this page so slow?",
			"thread_ts": "1111111111.111111",
			"channel":   "C0PRODUCT",
			"ts":        "2222222222.222222",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	// No phase transition should happen
	calls := phaseUpdater.getCalls()
	for _, c := range calls {
		if c.Phase == domain.PhaseRevision {
			t.Error("Unexpected transition to revision — question during review should not change phase")
		}
	}
}
