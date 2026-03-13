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

// LLMClient sends chat completion requests to an LLM API.
type LLMClient interface {
	ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error)
}

// FireworksClient implements LLMClient using the Fireworks OpenAI-compatible API.
type FireworksClient struct {
	httpClient *http.Client
	apiKey     string
	baseURL    string
	retry      RetryConfig
}

// NewFireworksClient creates a new Fireworks API client.
func NewFireworksClient(apiKey string) *FireworksClient {
	return &FireworksClient{
		httpClient: &http.Client{Timeout: 60 * time.Second},
		apiKey:     apiKey,
		baseURL:    "https://api.fireworks.ai/inference/v1",
		retry:      DefaultRetryConfig(),
	}
}

// NewFireworksClientWithConfig creates a new Fireworks API client with custom settings.
func NewFireworksClientWithConfig(apiKey string, timeout time.Duration, retry RetryConfig) *FireworksClient {
	return &FireworksClient{
		httpClient: &http.Client{Timeout: timeout},
		apiKey:     apiKey,
		baseURL:    "https://api.fireworks.ai/inference/v1",
		retry:      retry,
	}
}

// fireworksRequest is the OpenAI-compatible request body.
type fireworksRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []ToolDef `json:"tools,omitempty"`
}

// fireworksResponse is the OpenAI-compatible response body.
type fireworksResponse struct {
	Choices []struct {
		Message      fireworksMessage `json:"message"`
		FinishReason string           `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type fireworksMessage struct {
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ChatCompletion sends a chat completion request to Fireworks with retry logic.
func (c *FireworksClient) ChatCompletion(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := fireworksRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Tools:    req.Tools,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("marshal request: %w", err)
	}

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

		resp, err := c.doRequest(ctx, payload, attempt+1)
		if err == nil {
			return resp, nil
		}
		lastErr = err

		// Only retry on retryable HTTP errors
		if !isRetryableError(err) {
			return ChatResponse{}, err
		}
	}

	return ChatResponse{}, lastErr
}

// retryableHTTPError wraps an HTTP error with its status code for retry decisions.
type retryableHTTPError struct {
	statusCode int
	body       string
}

func (e *retryableHTTPError) Error() string {
	return fmt.Sprintf("api error (status %d): %s", e.statusCode, e.body)
}

// isRetryableError checks if an error is a retryable HTTP error.
func isRetryableError(err error) bool {
	if httpErr, ok := err.(*retryableHTTPError); ok {
		return isRetryable(httpErr.statusCode)
	}
	return false
}

// doRequest performs a single HTTP request to the Fireworks API.
func (c *FireworksClient) doRequest(ctx context.Context, payload []byte, attempt int) (ChatResponse, error) {
	start := time.Now()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		slog.Info("llm request failed", "attempt", attempt, "latency_ms", latencyMs, "error", err)
		return ChatResponse{}, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	slog.Info("llm request completed", "attempt", attempt, "status", resp.StatusCode, "latency_ms", latencyMs)

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, &retryableHTTPError{statusCode: resp.StatusCode, body: string(respBody)}
	}

	var fwResp fireworksResponse
	if err := json.Unmarshal(respBody, &fwResp); err != nil {
		return ChatResponse{}, fmt.Errorf("parse response: %w", err)
	}

	if fwResp.Error != nil {
		return ChatResponse{}, fmt.Errorf("api error: %s", fwResp.Error.Message)
	}

	if len(fwResp.Choices) == 0 {
		return ChatResponse{}, fmt.Errorf("no choices in response")
	}

	choice := fwResp.Choices[0]
	return ChatResponse{
		Content:      choice.Message.Content,
		ToolCalls:    choice.Message.ToolCalls,
		FinishReason: choice.FinishReason,
	}, nil
}
