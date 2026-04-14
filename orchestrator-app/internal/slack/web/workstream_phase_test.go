package web

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// mockPhaseUpdater tracks calls to UpdateWorkstreamPhase
type mockPhaseUpdater struct {
	mu    sync.Mutex
	calls []phaseUpdateCall
}

type phaseUpdateCall struct {
	ThreadTs string
	Phase    domain.WorkstreamPhase
}

func (m *mockPhaseUpdater) UpdateWorkstreamPhase(_ context.Context, threadTs string, phase domain.WorkstreamPhase) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, phaseUpdateCall{ThreadTs: threadTs, Phase: phase})
	return nil
}

func (m *mockPhaseUpdater) SetFeedbackRelayed(_ context.Context, _ string, _ bool) error {
	return nil
}

func (m *mockPhaseUpdater) getCalls() []phaseUpdateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestHandleEvent_PassesWorkstreamPhaseToAgent(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Got your feedback."},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID:   48,
			GithubPRID:      52,
			RepoOwner:       "example-org",
			RepoName:        "playground",
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

	h := NewHandler(agentRunner, threadFinder, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":      "app_mention",
			"user":      "U123",
			"text":      "<@UBOT> the sidebar is too wide",
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

	if !agentRunner.wasCalled() {
		t.Fatal("Expected agent to be called")
	}

	lastReq := agentRunner.getLastReq()
	if lastReq.WorkstreamPhase != domain.PhaseReview {
		t.Errorf("Expected WorkstreamPhase %q, got %q", domain.PhaseReview, lastReq.WorkstreamPhase)
	}
}

func TestHandleEvent_IssueCreated_TransitionsToOpen(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Created #48: Add dark mode",
			SideEffects: agent.SideEffects{
				CreatedIssues: []agent.CreatedIssue{{Number: 48, Title: "Add dark mode"}},
			},
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

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")
	h.SetPhaseUpdater(phaseUpdater)

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> add dark mode support",
			"channel": "C0PRODUCT",
			"ts":      "3333333333.333333",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	calls := phaseUpdater.getCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase update after issue creation")
	}
	// The thread ts for top-level mentions is the event ts
	if calls[0].Phase != domain.PhaseOpen {
		t.Errorf("Expected phase %q, got %q", domain.PhaseOpen, calls[0].Phase)
	}
}

func TestHandleEvent_FeedbackInReview_TransitionsToRevision(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Got it, I've passed that along.",
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
			"text":      "<@UBOT> the sidebar is too wide",
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

	calls := phaseUpdater.getCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase update after feedback relay")
	}
	found := false
	for _, c := range calls {
		if c.Phase == domain.PhaseRevision {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected phase transition to revision, got: %v", calls)
	}
}

func TestHandleEvent_NoThread_EmptyPhase(t *testing.T) {
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{Text: "Hello!"},
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

	h := NewHandler(agentRunner, &mockThreadFinder{}, &mockThreadSaver{}, nil, slackClient, registry, testSigningSecret, "")

	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"user":    "U123",
			"text":    "<@UBOT> add dark mode",
			"channel": "C0PRODUCT",
			"ts":      "3333333333.333333",
		},
		"authorizations": []map[string]string{{"user_id": "UBOT"}},
	}
	body, _ := json.Marshal(payload)
	req := makeSignedRequest(t, body)
	rec := httptest.NewRecorder()

	h.HandleEvent(rec, req)
	time.Sleep(200 * time.Millisecond)

	if !agentRunner.wasCalled() {
		t.Fatal("Expected agent to be called")
	}

	lastReq := agentRunner.getLastReq()
	if lastReq.WorkstreamPhase != "" {
		t.Errorf("Expected empty WorkstreamPhase for new thread, got %q", lastReq.WorkstreamPhase)
	}
}
