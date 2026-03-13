package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
}

// NewFireworksClient creates a new Fireworks API client.
func NewFireworksClient(apiKey string) *FireworksClient {
	return &FireworksClient{
		httpClient: &http.Client{},
		apiKey:     apiKey,
		baseURL:    "https://api.fireworks.ai/inference/v1",
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

// ChatCompletion sends a chat completion request to Fireworks.
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

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return ChatResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
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
