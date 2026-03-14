package domain

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

func TestRouter_SingleStepResearch(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"info request"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "what issues are open?", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "researcher" {
		t.Errorf("expected researcher, got %s", decision.Steps[0].Specialist)
	}
}

func TestRouter_SingleStepIssueCreation(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"user wants new issue"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "create an issue for dark mode", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "issue_creator" {
		t.Errorf("expected issue_creator, got %s", decision.Steps[0].Specialist)
	}
}

func TestRouter_MultiStep_CreateAndDelegate(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"issue_creator","params":{},"reasoning":"create issue"},{"specialist":"delegator","params":{},"reasoning":"delegate to opencode"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "create an issue and delegate to OpenCode", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "issue_creator" {
		t.Errorf("step 0: expected issue_creator, got %s", decision.Steps[0].Specialist)
	}
	if decision.Steps[1].Specialist != "delegator" {
		t.Errorf("step 1: expected delegator, got %s", decision.Steps[1].Specialist)
	}
}

func TestRouter_MalformedJSON_FallsBackToResearcher(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "I'm not sure what JSON is, here's some garbled text!", FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "hello", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "researcher" {
		t.Errorf("expected researcher fallback, got %s", decision.Steps[0].Specialist)
	}
}

func TestRouter_EmptySteps_FallsBackToResearcher(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "hello", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 1 {
		t.Fatalf("expected 1 fallback step, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "researcher" {
		t.Errorf("expected researcher fallback, got %s", decision.Steps[0].Specialist)
	}
}

func TestRouter_LLMError_PropagatesError(t *testing.T) {
	llm := &mockLLMClient{
		errors: []error{fmt.Errorf("connection refused")},
	}
	r := NewRouter(llm, "test-model")

	_, err := r.Route(context.Background(), "hello", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection error, got: %v", err)
	}
}

func TestRouter_IncludesLinkedIssueInPrompt(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"ok"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	_, err := r.Route(context.Background(), "what's the status?", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, &IssueContext{Number: 42, Title: "Login bug"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the LLM request messages mention the linked issue
	if len(llm.requests) != 1 {
		t.Fatalf("expected 1 LLM request, got %d", len(llm.requests))
	}
	found := false
	for _, msg := range llm.requests[0].Messages {
		if strings.Contains(msg.Content, "#42") || strings.Contains(msg.Content, "Login bug") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected linked issue context in router prompt")
	}
}

func TestRouter_PromptContainsRepoInfo(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: `{"steps":[{"specialist":"researcher","params":{},"reasoning":"ok"}]}`, FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	_, err := r.Route(context.Background(), "hello", targets.TargetConfig{
		RepoOwner: "myorg", RepoName: "myrepo",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(llm.requests) != 1 {
		t.Fatalf("expected 1 LLM request, got %d", len(llm.requests))
	}
	found := false
	for _, msg := range llm.requests[0].Messages {
		if strings.Contains(msg.Content, "myorg") && strings.Contains(msg.Content, "myrepo") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected repo owner/name in router prompt")
	}
}

func TestRouter_JSONInCodeFence(t *testing.T) {
	llm := &mockLLMClient{
		responses: []ChatResponse{
			{Content: "```json\n{\"steps\":[{\"specialist\":\"closer\",\"params\":{\"number\":\"7\"},\"reasoning\":\"close it\"}]}\n```", FinishReason: "stop"},
		},
	}
	r := NewRouter(llm, "test-model")

	decision, err := r.Route(context.Background(), "close issue #7", targets.TargetConfig{
		RepoOwner: "acme", RepoName: "widgets",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decision.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(decision.Steps))
	}
	if decision.Steps[0].Specialist != "closer" {
		t.Errorf("expected closer, got %s", decision.Steps[0].Specialist)
	}
}
