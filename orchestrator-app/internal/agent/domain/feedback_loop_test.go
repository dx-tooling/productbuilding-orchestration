package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

func TestRouter_ReviewPhase_FeedbackRoutesDelegator(t *testing.T) {
	// When phase is review and user gives feedback, router should route to delegator
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"delegator","params":{},"reasoning":"relay feedback on preview"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	_, err := r.Route(context.Background(), "the sidebar is too wide", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, &IssueContext{Number: 52, Title: "Add dark mode"}, nil, slackdomain.PhaseReview)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the prompt contains both phase and linked issue
	userMsg := llm.requests[0].Messages[len(llm.requests[0].Messages)-1].Content
	if !strings.Contains(userMsg, "Workstream phase: review") {
		t.Error("expected phase signal in user message")
	}
	if !strings.Contains(userMsg, "#52") {
		t.Error("expected linked issue in user message")
	}
}

func TestRouter_ReviewPhase_ApprovalRoutesDelegator(t *testing.T) {
	// When phase is review and user approves, router should route to delegator (to mark PR ready)
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"delegator","params":{},"reasoning":"user approves, mark PR ready for developer review"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	_, err := r.Route(context.Background(), "looks good, ship it", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, &IssueContext{Number: 52, Title: "Add dark mode"}, nil, slackdomain.PhaseReview)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if llm.requests[0].Messages[len(llm.requests[0].Messages)-1].Role != "user" {
		t.Error("expected user message as last message")
	}
}

func TestRouter_IntakePhase_FollowupRoutesToIssueCreator(t *testing.T) {
	// When phase is intake and user responds, router should route to issue_creator
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"user answered clarifying question"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	threadMsgs := []ThreadMessage{
		{User: "U001", Text: "add dark mode", Ts: "100.000"},
		{User: "B001", Text: "Should dark mode cover the whole app or just settings?", Ts: "100.001", BotID: "B001"},
		{User: "U001", Text: "whole app, follow OS setting", Ts: "100.002"},
	}

	decision, err := r.Route(context.Background(), "whole app, follow OS setting", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil, threadMsgs, slackdomain.PhaseIntake)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decision.Steps) == 0 {
		t.Fatal("expected at least one step")
	}
	if decision.Steps[0].Specialist != "issue_creator" {
		t.Errorf("expected issue_creator, got %s", decision.Steps[0].Specialist)
	}
}
