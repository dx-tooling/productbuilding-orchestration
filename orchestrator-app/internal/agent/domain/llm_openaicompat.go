package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// OpenAICompatClient implements LLMClient for the OpenAI Chat Completions API
// using raw HTTP. Covers OpenAI, Fireworks, and any OpenAI-compatible provider.
type OpenAICompatClient struct {
	httpClient *http.Client
	apiKey     string
	model      string
	baseURL    string
	retry      RetryConfig
}

// NewOpenAICompatClient creates a new OpenAI-compatible client.
// baseURL should be the API root (e.g. "https://api.openai.com/v1" or
// "https://api.fireworks.ai/inference/v1").
func NewOpenAICompatClient(apiKey, model, baseURL string) *OpenAICompatClient {
	return &OpenAICompatClient{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		retry:      DefaultRetryConfig(),
	}
}

// NewOpenAICompatClientWithConfig creates a new OpenAI-compatible client with custom retry config.
func NewOpenAICompatClientWithConfig(apiKey, model, baseURL string, retry RetryConfig) *OpenAICompatClient {
	return &OpenAICompatClient{
		httpClient: &http.Client{Timeout: 120 * time.Second},
		apiKey:     apiKey,
		model:      model,
		baseURL:    baseURL,
		retry:      retry,
	}
}

// openaiRequest is the request body for the OpenAI chat completions endpoint.
type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Tools    []ToolDef       `json:"tools,omitempty"`
}

// openaiMessage represents a message in the OpenAI format.
type openaiMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// openaiResponse is the response from the OpenAI chat completions endpoint.
type openaiResponse struct {
	Choices []openaiChoice `json:"choices"`
	Error   *openaiError   `json:"error,omitempty"`
}

type openaiChoice struct {
	Message      openaiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// ChatCompletion sends a chat completion request to an OpenAI-compatible endpoint.
func (c *OpenAICompatClient) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body, err := c.buildRequestBody(req)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	maxAttempts := 1 + c.retry.MaxRetries

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := backoff(attempt-1, c.retry)
			slog.Info("openaicompat retry backoff", "attempt", attempt+1, "delay_ms", delay.Milliseconds())
			if err := sleepWithContext(ctx, delay); err != nil {
				return ChatResponse{}, fmt.Errorf("retry cancelled: %w", err)
			}
		}

		start := time.Now()
		resp, statusCode, err := c.doRequest(ctx, body)
		latencyMs := time.Since(start).Milliseconds()

		if err != nil {
			lastErr = err
			slog.Info("openaicompat request failed", "attempt", attempt+1, "latency_ms", latencyMs, "error", err)
			// Network errors are not retryable
			return ChatResponse{}, fmt.Errorf("api request: %w", err)
		}

		if statusCode >= 200 && statusCode < 300 {
			slog.Info("openaicompat request completed", "attempt", attempt+1, "latency_ms", latencyMs)
			return c.translateResponse(resp)
		}

		lastErr = fmt.Errorf("api error (status %d): %s", statusCode, extractErrorMessage(resp))
		slog.Info("openaicompat request failed", "attempt", attempt+1, "status", statusCode, "latency_ms", latencyMs)

		if !isRetryable(statusCode) {
			return ChatResponse{}, lastErr
		}
	}

	return ChatResponse{}, &ProviderUnavailableError{Provider: "openaicompat", Err: lastErr}
}

func (c *OpenAICompatClient) buildRequestBody(req ChatRequest) ([]byte, error) {
	oaiReq := openaiRequest{
		Model: c.model,
		Tools: req.Tools,
	}

	for _, msg := range req.Messages {
		oaiMsg := openaiMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  msg.ToolCalls,
			ToolCallID: msg.ToolCallID,
		}
		oaiReq.Messages = append(oaiReq.Messages, oaiMsg)
	}

	return json.Marshal(oaiReq)
}

func (c *OpenAICompatClient) doRequest(ctx context.Context, body []byte) ([]byte, int, error) {
	url := c.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, httpResp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, httpResp.StatusCode, nil
}

func (c *OpenAICompatClient) translateResponse(body []byte) (ChatResponse, error) {
	var oaiResp openaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return ChatResponse{}, fmt.Errorf("unmarshal response: %w", err)
	}

	if oaiResp.Error != nil {
		return ChatResponse{}, fmt.Errorf("api error: %s", oaiResp.Error.Message)
	}

	if len(oaiResp.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("empty choices in response")
	}

	choice := oaiResp.Choices[0]
	resp := ChatResponse{
		Content:      choice.Message.Content,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
	}

	return resp, nil
}

func extractErrorMessage(body []byte) string {
	var oaiResp openaiResponse
	if err := json.Unmarshal(body, &oaiResp); err == nil && oaiResp.Error != nil {
		return oaiResp.Error.Message
	}
	if len(body) > 200 {
		return string(body[:200])
	}
	return string(body)
}
