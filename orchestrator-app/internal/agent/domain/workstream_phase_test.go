package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	slackdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

func TestRunRequest_HasWorkstreamPhaseField(t *testing.T) {
	req := RunRequest{
		WorkstreamPhase: slackdomain.PhaseReview,
	}
	if req.WorkstreamPhase != slackdomain.PhaseReview {
		t.Errorf("expected phase %q, got %q", slackdomain.PhaseReview, req.WorkstreamPhase)
	}
}

func TestRouter_IncludesWorkstreamPhaseInPrompt(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"delegator","params":{},"reasoning":"feedback on preview"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	_, err := r.Route(context.Background(), "the sidebar is too wide", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil, nil, slackdomain.PhaseReview)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) != 1 {
		t.Fatalf("expected 1 LLM request, got %d", len(llm.requests))
	}

	found := false
	for _, msg := range llm.requests[0].Messages {
		if strings.Contains(msg.Content, "Workstream phase: review") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected workstream phase in router prompt")
	}
}

func TestRouter_EmptyPhaseNotIncluded(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"ok"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	_, err := r.Route(context.Background(), "hello", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil, nil, "")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only check the user message (last message), not the system prompt
	userMsg := llm.requests[0].Messages[len(llm.requests[0].Messages)-1]
	if userMsg.Role != "user" {
		t.Fatalf("expected last message to be user, got %s", userMsg.Role)
	}
	if strings.Contains(userMsg.Content, "[Workstream phase:") {
		t.Error("empty phase should not produce a workstream phase signal in the user message")
	}
}

func TestRouter_PhaseReviewGuidesRouting(t *testing.T) {
	// Verify the router prompt includes phase-specific guidance for routing
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"delegator","params":{},"reasoning":"relay feedback"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm)

	_, err := r.Route(context.Background(), "the colors are off", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, &IssueContext{Number: 48, Title: "Add dark mode"}, nil, slackdomain.PhaseReview)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check the user message contains both phase and linked issue
	userMsg := llm.requests[0].Messages[len(llm.requests[0].Messages)-1].Content
	if !strings.Contains(userMsg, "Workstream phase: review") {
		t.Error("expected workstream phase in user message")
	}
	if !strings.Contains(userMsg, "#48") {
		t.Error("expected linked issue in user message")
	}
}
