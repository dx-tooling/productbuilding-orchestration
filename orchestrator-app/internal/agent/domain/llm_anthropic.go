package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient implements LLMClient using the Anthropic Messages API.
type AnthropicClient struct {
	client anthropic.Client
	model  string
	retry  RetryConfig
}

// NewAnthropicClient creates a new Anthropic API client.
func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	return &AnthropicClient{
		client: anthropic.NewClient(
			option.WithAPIKey(apiKey),
			option.WithMaxRetries(0), // We handle retries ourselves
		),
		model: model,
		retry: DefaultRetryConfig(),
	}
}

// NewAnthropicClientWithConfig creates a new Anthropic API client with custom retry and base URL.
func NewAnthropicClientWithConfig(apiKey, model string, retry RetryConfig, baseURL string) *AnthropicClient {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithMaxRetries(0),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &AnthropicClient{
		client: anthropic.NewClient(opts...),
		model:  model,
		retry:  retry,
	}
}

// ChatCompletion sends a chat completion request to the Anthropic Messages API with retry logic.
func (c *AnthropicClient) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	params := c.buildParams(req)

	var lastErr error
	maxAttempts := 1 + c.retry.MaxRetries

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt-1, c.retry)
			slog.Info("llm retry backoff", "attempt", attempt+1, "delay_ms", delay.Milliseconds())
			if err := sleepWithContext(ctx, delay); err != nil {
				return ChatResponse{}, fmt.Errorf("retry cancelled: %w", err)
			}
		}

		start := time.Now()
		msg, err := c.client.Messages.New(ctx, params)
		latencyMs := time.Since(start).Milliseconds()

		if err == nil {
			slog.Info("llm request completed", "attempt", attempt+1, "latency_ms", latencyMs)
			return c.translateResponse(msg), nil
		}

		lastErr = err

		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			slog.Info("llm request failed", "attempt", attempt+1, "status", apiErr.StatusCode, "latency_ms", latencyMs)
			if !isRetryable(apiErr.StatusCode) {
				return ChatResponse{}, fmt.Errorf("api error (status %d): %w", apiErr.StatusCode, err)
			}
		} else {
			slog.Info("llm request failed", "attempt", attempt+1, "latency_ms", latencyMs, "error", err)
			return ChatResponse{}, fmt.Errorf("api request: %w", err)
		}
	}

	return ChatResponse{}, &ProviderUnavailableError{Provider: "anthropic", Err: lastErr}
}

// buildParams translates a ChatRequest into Anthropic MessageNewParams.
func (c *AnthropicClient) buildParams(req ChatRequest) anthropic.MessageNewParams {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 16384,
	}

	// Extract system messages and convert remaining messages
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			params.System = append(params.System, anthropic.TextBlockParam{Text: msg.Content})
		case "user":
			params.Messages = append(params.Messages,
				anthropic.NewUserMessage(anthropic.NewTextBlock(msg.Content)),
			)
		case "assistant":
			if len(msg.ToolCalls) > 0 {
				var blocks []anthropic.ContentBlockParamUnion
				if msg.Content != "" {
					blocks = append(blocks, anthropic.NewTextBlock(msg.Content))
				}
				for _, tc := range msg.ToolCalls {
					var input any
					if tc.Function.Arguments != "" {
						json.Unmarshal([]byte(tc.Function.Arguments), &input)
					}
					if input == nil {
						input = map[string]any{}
					}
					blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
				}
				params.Messages = append(params.Messages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: blocks,
				})
			} else {
				params.Messages = append(params.Messages,
					anthropic.NewAssistantMessage(anthropic.NewTextBlock(msg.Content)),
				)
			}
		case "tool":
			params.Messages = append(params.Messages, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(msg.ToolCallID, msg.Content, false),
			))
		}
	}

	// Convert tool definitions
	for _, td := range req.Tools {
		var props any
		var required []string
		var schema map[string]any
		if err := json.Unmarshal(td.Function.Parameters, &schema); err == nil {
			props = schema["properties"]
			if reqFields, ok := schema["required"].([]any); ok {
				for _, r := range reqFields {
					if s, ok := r.(string); ok {
						required = append(required, s)
					}
				}
			}
		}

		tool := anthropic.ToolUnionParamOfTool(
			anthropic.ToolInputSchemaParam{
				Properties: props,
				Required:   required,
			},
			td.Function.Name,
		)
		if td.Function.Description != "" {
			tool.OfTool.Description = anthropic.String(td.Function.Description)
		}
		params.Tools = append(params.Tools, tool)
	}

	return params
}

// translateResponse converts an Anthropic Message into a ChatResponse.
func (c *AnthropicClient) translateResponse(msg *anthropic.Message) ChatResponse {
	resp := ChatResponse{}

	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			resp.Content = v.Text
		case anthropic.ToolUseBlock:
			resp.ToolCalls = append(resp.ToolCalls, ToolCall{
				ID:   v.ID,
				Type: "function",
				Function: FunctionCall{
					Name:      v.Name,
					Arguments: string(v.Input),
				},
			})
		}
	}

	switch msg.StopReason {
	case anthropic.StopReasonEndTurn:
		resp.FinishReason = "stop"
	case anthropic.StopReasonToolUse:
		resp.FinishReason = "tool_calls"
	case anthropic.StopReasonMaxTokens:
		resp.FinishReason = "length"
	default:
		resp.FinishReason = string(msg.StopReason)
	}

	return resp
}
