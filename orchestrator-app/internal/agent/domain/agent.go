package domain

import (
	"context"
	"fmt"
	"log/slog"

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
	llm          LLMClient
	tools        ToolExecutor
	slackFetcher SlackThreadFetcher
	model        string
}

// NewAgent creates a new agent with the given dependencies.
func NewAgent(llm LLMClient, tools ToolExecutor, slackFetcher SlackThreadFetcher, model string) *Agent {
	return &Agent{
		llm:          llm,
		tools:        tools,
		slackFetcher: slackFetcher,
		model:        model,
	}
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
			return RunResponse{
				Text:        resp.Content,
				SideEffects: a.tools.Effects(),
			}, nil
		}

		// Append assistant message with tool calls
		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		// Execute each tool call
		for _, tc := range resp.ToolCalls {
			result, err := a.tools.Execute(ctx, tc, req.Target)
			if err != nil {
				slog.Warn("tool execution failed", "tool", tc.Function.Name, "error", err)
				result = fmt.Sprintf("Error: %s", err.Error())
			}

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Max iterations reached
	return RunResponse{
		Text:        "I'm having trouble processing this request. Please try again or rephrase your question.",
		SideEffects: a.tools.Effects(),
	}, nil
}
