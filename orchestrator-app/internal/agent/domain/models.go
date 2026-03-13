package domain

import "encoding/json"

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
	Model    string    `json:"model"`
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
	CreatedIssues  []CreatedIssue
	PostedComments []int64
}

// IssueContext provides linked GitHub issue data to the agent.
type IssueContext struct {
	Number int
	Title  string
	Body   string
	State  string
}
