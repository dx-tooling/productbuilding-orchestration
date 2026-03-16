package domain

import (
	"context"
	"testing"
)

func TestWithTrace_RoundTrip(t *testing.T) {
	trace := &Trace{}
	ctx := WithTrace(context.Background(), trace)
	got := TraceFromContext(ctx)
	if got != trace {
		t.Errorf("expected same trace pointer, got %v", got)
	}
}

func TestTraceFromContext_EmptyContext(t *testing.T) {
	got := TraceFromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil trace from empty context, got %v", got)
	}
}

func TestTrace_RecordRouting(t *testing.T) {
	trace := &Trace{}
	trace.Routing = &RoutingTrace{
		OutputText: "researcher",
		LatencyMs:  150,
		InputMessages: []TraceLLMMessage{
			{Role: "system", Content: "You are a router..."},
		},
	}
	if trace.Routing.OutputText != "researcher" {
		t.Errorf("expected routing output 'researcher', got %q", trace.Routing.OutputText)
	}
	if len(trace.Routing.InputMessages) != 1 {
		t.Fatalf("expected 1 input message, got %d", len(trace.Routing.InputMessages))
	}
}

func TestTrace_RecordStep(t *testing.T) {
	trace := &Trace{}
	step := StepTrace{
		Specialist: "researcher",
		Iterations: []IterationTrace{
			{
				MessageCount: 18,
				LLMContent:   "Let me check the CI status.",
				FinishReason: "tool_calls",
				LatencyMs:    4434,
				ToolCalls: []ToolCallTrace{
					{
						Name:      "list_workflow_runs",
						Arguments: `{"owner":"luminor-project","repo":"playground"}`,
						Result:    `[{"id":123,"status":"completed"}]`,
						LatencyMs: 200,
					},
					{
						Name:      "get_workflow_run_jobs",
						Arguments: `{"owner":"luminor-project","repo":"playground","run_id":123}`,
						Error:     "status 404: Not Found",
						LatencyMs: 187,
					},
				},
			},
		},
	}
	trace.Steps = append(trace.Steps, step)

	if len(trace.Steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(trace.Steps))
	}
	if trace.Steps[0].Specialist != "researcher" {
		t.Errorf("expected specialist 'researcher', got %q", trace.Steps[0].Specialist)
	}
	iter := trace.Steps[0].Iterations[0]
	if len(iter.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(iter.ToolCalls))
	}
	if iter.ToolCalls[1].Error != "status 404: Not Found" {
		t.Errorf("expected error on second tool call, got %q", iter.ToolCalls[1].Error)
	}
}
