package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

func TestHandleEvent_NewRequest_NoIssueCreated_TransitionsToIntake(t *testing.T) {
	// When the agent responds without creating an issue (asked clarifying questions),
	// the handler should transition to "intake" phase.
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Before I create a ticket — should dark mode cover the entire app or just a specific section?",
			// No CreatedIssues → agent asked a question instead
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

	calls := phaseUpdater.getCalls()
	if len(calls) == 0 {
		t.Fatal("Expected phase transition to intake when agent asks clarifying questions")
	}
	if calls[0].Phase != domain.PhaseIntake {
		t.Errorf("Expected phase %q, got %q", domain.PhaseIntake, calls[0].Phase)
	}
}

func TestHandleEvent_IntakePhase_IssueCreated_TransitionsToOpen(t *testing.T) {
	// When the user responds to clarifying questions and the agent creates an issue,
	// the phase should go from intake to open.
	agentRunner := &mockAgentRunner{
		response: agent.RunResponse{
			Text: "Created #48: Add system-wide dark mode following OS preference.",
			SideEffects: agent.SideEffects{
				CreatedIssues: []agent.CreatedIssue{{Number: 48, Title: "Add system-wide dark mode"}},
			},
		},
	}
	threadFinder := &mockThreadFinder{
		thread: &domain.SlackThread{
			GithubIssueID:   0, // No issue yet
			RepoOwner:       "example-org",
			RepoName:        "playground",
			SlackThreadTs:   "1111111111.111111",
			WorkstreamPhase: domain.PhaseIntake,
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
			"text":      "<@UBOT> whole app, follow OS setting",
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
		t.Fatal("Expected phase transition to open after issue creation from intake")
	}
	if calls[0].Phase != domain.PhaseOpen {
		t.Errorf("Expected phase %q, got %q", domain.PhaseOpen, calls[0].Phase)
	}
}
