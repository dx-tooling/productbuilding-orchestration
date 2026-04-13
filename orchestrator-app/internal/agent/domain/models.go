package domain

import (
	"context"
	"encoding/json"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// SlackThreadFetcher retrieves thread history from Slack.
type SlackThreadFetcher interface {
	GetThreadReplies(ctx context.Context, botToken, channel, threadTs string) ([]ThreadMessage, error)
}

// ThreadMessage represents a single message from a Slack thread.
type ThreadMessage struct {
	User  string `json:"user"`
	Text  string `json:"text"`
	Ts    string `json:"ts"`
	BotID string `json:"bot_id,omitempty"`
}

// RunRequest contains the context for a single agent invocation.
type RunRequest struct {
	ChannelID      string
	ThreadTs       string
	MessageTs      string
	UserText       string
	UserName       string
	BotUserID      string
	Target         targets.TargetConfig
	LinkedIssue    *IssueContext
	ThreadMessages []ThreadMessage // Pre-fetched thread context; if set, specialist skips fetching
	FeatureSummary string          // Pre-formatted feature context summary for LLM
}

// RunResponse is returned after the agent completes.
type RunResponse struct {
	Text        string
	SideEffects SideEffects
}

// Message represents a chat message in the LLM conversation.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call requested by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall holds the name and arguments of a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolDef describes a tool available to the LLM.
type ToolDef struct {
	Type     string     `json:"type"`
	Function ToolSchema `json:"function"`
}

// ToolSchema describes the function name, description, and parameters.
type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ChatRequest is sent to the LLM API.
type ChatRequest struct {
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// ChatResponse is returned from the LLM API.
type ChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"`
}

// CreatedIssue records an issue created by the agent.
type CreatedIssue struct {
	Number int
	Title  string
}

// SideEffects tracks observable actions performed during agent execution.
type SideEffects struct {
	CreatedIssues   []CreatedIssue
	PostedComments  []int64
	DelegatedIssues []int
}

// IssueContext provides linked GitHub issue data to the agent.
type IssueContext struct {
	Number int
	Title  string
	Body   string
	State  string
}

// RoutingDecision is the Router's output: one or more specialist steps.
type RoutingDecision struct {
	Steps []RoutingStep `json:"steps"`
}

// RoutingStep identifies a specialist to invoke with optional parameters.
type RoutingStep struct {
	Specialist string            `json:"specialist"`
	Params     map[string]string `json:"params"`
	Reasoning  string            `json:"reasoning"`
}

// PriorStepContext carries output from a preceding specialist step in a chain.
type PriorStepContext struct {
	StepName   string
	ResultText string
	Effects    SideEffects
}

// SpecialistResult is returned by a specialist after execution.
type SpecialistResult struct {
	Text        string
	SideEffects SideEffects
	Reroute     string // If non-empty, the orchestrator should invoke this specialist instead
}

// PromptData holds the values injected into system prompt templates.
type PromptData struct {
	RepoOwner string
	RepoName  string
}
