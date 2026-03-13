package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

const maxIterations = 10

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

// Agent is the core orchestration loop: system prompt + thread context → LLM → tool calls → response.
type Agent struct {
	llm                 LLMClient
	tools               ToolExecutor
	slackFetcher        SlackThreadFetcher
	model               string
	conversationLister  ConversationLister
	workspace           string
}

// AgentOption configures optional Agent dependencies.
type AgentOption func(*Agent)

// WithConversationLister adds conversation listing support to the agent.
func WithConversationLister(lister ConversationLister, workspace string) AgentOption {
	return func(a *Agent) {
		a.conversationLister = lister
		a.workspace = workspace
	}
}

// NewAgent creates a new agent with the given dependencies.
func NewAgent(llm LLMClient, tools ToolExecutor, slackFetcher SlackThreadFetcher, model string, opts ...AgentOption) *Agent {
	a := &Agent{
		llm:          llm,
		tools:        tools,
		slackFetcher: slackFetcher,
		model:        model,
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// RunRequest contains the context for a single agent invocation.
type RunRequest struct {
	ChannelID string
	ThreadTs  string
	MessageTs string
	UserText  string
	UserName  string
	BotUserID string
	Target    targets.TargetConfig
	LinkedIssue *IssueContext
}

// RunResponse is returned after the agent completes.
type RunResponse struct {
	Text        string
	SideEffects SideEffects
}

// Run executes the agent loop: build context → call LLM → execute tools → return response.
func (a *Agent) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	systemPrompt, err := RenderSystemPrompt(PromptData{
		RepoOwner: req.Target.RepoOwner,
		RepoName:  req.Target.RepoName,
	})
	if err != nil {
		return RunResponse{}, fmt.Errorf("render system prompt: %w", err)
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
	}

	// Fetch thread context if this is an in-thread mention
	if req.ThreadTs != "" && a.slackFetcher != nil {
		threadMsgs, err := a.slackFetcher.GetThreadReplies(ctx, req.Target.SlackBotToken, req.ChannelID, req.ThreadTs)
		if err != nil {
			slog.Warn("failed to fetch thread replies", "error", err)
		} else {
			for _, msg := range threadMsgs {
				// Skip the current message (we add it below)
				if msg.Ts == req.MessageTs {
					continue
				}
				role := "user"
				if msg.BotID != "" {
					role = "assistant"
				}
				messages = append(messages, Message{Role: role, Content: msg.Text})
			}
		}
	}

	// Add linked issue context if available
	if req.LinkedIssue != nil {
		issueCtx := fmt.Sprintf("[Context: This thread is linked to GitHub issue #%d (%s) — state: %s]\n\n%s",
			req.LinkedIssue.Number, req.LinkedIssue.Title, req.LinkedIssue.State, req.LinkedIssue.Body)
		messages = append(messages, Message{Role: "system", Content: issueCtx})
	}

	// Add the current user message
	messages = append(messages, Message{
		Role:    "user",
		Content: fmt.Sprintf("%s says: %s", req.UserName, req.UserText),
	})

	tools := ToolDefinitions()

	// Agent loop
	for i := 0; i < maxIterations; i++ {
		slog.Info("agent llm call",
			"iteration", i+1,
			"channel", req.ChannelID,
			"user", req.UserName,
			"message_count", len(messages),
		)

		resp, err := a.llm.ChatCompletion(ctx, ChatRequest{
			Model:    a.model,
			Messages: messages,
			Tools:    tools,
		})
		if err != nil {
			return RunResponse{}, fmt.Errorf("llm completion: %w", err)
		}

		// If the LLM returned a text response (no tool calls), we're done
		if resp.FinishReason == "stop" || len(resp.ToolCalls) == 0 {
			slog.Info("agent finished",
				"channel", req.ChannelID,
				"user", req.UserName,
				"finish_reason", resp.FinishReason,
				"tool_calls_made", i > 0,
				"response_length", len(resp.Content),
			)
			return RunResponse{
				Text:        resp.Content,
				SideEffects: a.tools.Effects(),
			}, nil
		}

		// Log each tool call the LLM requested
		for _, tc := range resp.ToolCalls {
			slog.Info("agent tool call requested",
				"channel", req.ChannelID,
				"user", req.UserName,
				"tool", tc.Function.Name,
				"tool_call_id", tc.ID,
				"arguments", tc.Function.Arguments,
			)
		}

		// Append assistant message with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			var result string
			var execErr error

			if tc.Function.Name == "list_conversations" && a.conversationLister != nil {
				result, execErr = a.executeListConversations(ctx, tc, req.ChannelID)
			} else {
				result, execErr = a.tools.Execute(ctx, tc, req.Target)
			}

			if execErr != nil {
				slog.Warn("agent tool execution failed",
					"channel", req.ChannelID,
					"tool", tc.Function.Name,
					"tool_call_id", tc.ID,
					"error", execErr,
				)
				result = fmt.Sprintf("Error: %s", execErr.Error())
			} else {
				slog.Info("agent tool execution succeeded",
					"channel", req.ChannelID,
					"tool", tc.Function.Name,
					"tool_call_id", tc.ID,
					"result_length", len(result),
				)
			}

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Max iterations reached
	slog.Warn("agent max iterations reached",
		"channel", req.ChannelID,
		"user", req.UserName,
		"max_iterations", maxIterations,
	)
	return RunResponse{
		Text:        "I'm having trouble processing this request. Please try again or rephrase your question.",
		SideEffects: a.tools.Effects(),
	}, nil
}

// executeListConversations handles the list_conversations tool call.
func (a *Agent) executeListConversations(ctx context.Context, tc ToolCall, channelID string) (string, error) {
	var args struct {
		Days int `json:"days"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}
	if args.Days <= 0 {
		args.Days = 14
	}

	convs, err := a.conversationLister.ListRecentConversations(ctx, channelID, args.Days)
	if err != nil {
		return "", fmt.Errorf("list conversations: %w", err)
	}

	if len(convs) == 0 {
		return fmt.Sprintf("No conversations found in this channel in the last %d days.", args.Days), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d conversation(s) in the last %d days:\n\n", len(convs), args.Days))
	for _, c := range convs {
		link := SlackThreadLink(a.workspace, c.ChannelID, c.ThreadTs)
		summary := c.Summary
		if summary == "" {
			summary = "(no summary)"
		}
		sb.WriteString(fmt.Sprintf("- %s (by %s) — <%s|view thread>\n", summary, c.UserName, link))
	}
	return sb.String(), nil
}
