package domain

import "context"

type contextKey string

const traceContextKey contextKey = "agent-trace"

// Trace captures the full execution trace of one agent turn.
type Trace struct {
	Routing  *RoutingTrace
	Steps    []StepTrace
	Response string
	Error    string
}

type RoutingTrace struct {
	InputMessages []TraceLLMMessage
	OutputText    string
	LatencyMs     int64
}

type StepTrace struct {
	Specialist string
	Iterations []IterationTrace
}

type IterationTrace struct {
	MessageCount  int
	InputMessages []TraceLLMMessage
	LLMContent    string
	ToolCalls     []ToolCallTrace
	LatencyMs     int64
	FinishReason  string
}

type ToolCallTrace struct {
	Name      string
	Arguments string
	Result    string
	Error     string
	LatencyMs int64
}

type TraceLLMMessage struct {
	Role    string
	Content string
}

// WithTrace attaches a Trace to the context.
func WithTrace(ctx context.Context, t *Trace) context.Context {
	return context.WithValue(ctx, traceContextKey, t)
}

// TraceFromContext retrieves the Trace from the context, or nil if absent.
func TraceFromContext(ctx context.Context) *Trace {
	t, _ := ctx.Value(traceContextKey).(*Trace)
	return t
}
