package domain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFireworksClient_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		json.NewEncoder(w).Encode(fireworksResponse{
			Choices: []struct {
				Message      fireworksMessage `json:"message"`
				FinishReason string           `json:"finish_reason"`
			}{
				{
					Message:      fireworksMessage{Content: "Hello, world!"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
	}

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", resp.FinishReason)
	}
}

func TestFireworksClient_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(fireworksResponse{
			Choices: []struct {
				Message      fireworksMessage `json:"message"`
				FinishReason string           `json:"finish_reason"`
			}{
				{
					Message: fireworksMessage{
						ToolCalls: []ToolCall{
							{
								ID:   "call_1",
								Type: "function",
								Function: FunctionCall{
									Name:      "create_github_issue",
									Arguments: `{"title":"Bug fix","body":"Fix the login"}`,
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
	}

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Create an issue"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Name != "create_github_issue" {
		t.Errorf("expected tool name 'create_github_issue', got %q", resp.ToolCalls[0].Function.Name)
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %q", resp.FinishReason)
	}
}

func TestFireworksClient_HTTPError_NonRetryable(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
		retry:      RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	}

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 call (no retries for 400), got %d", callCount)
	}
}

func TestFireworksClient_Retry_429ThenSuccess(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		json.NewEncoder(w).Encode(fireworksResponse{
			Choices: []struct {
				Message      fireworksMessage `json:"message"`
				FinishReason string           `json:"finish_reason"`
			}{
				{
					Message:      fireworksMessage{Content: "Success after retries"},
					FinishReason: "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
		retry:      RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	}

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Success after retries" {
		t.Errorf("expected 'Success after retries', got %q", resp.Content)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2 retries + success), got %d", callCount)
	}
}

func TestFireworksClient_Retry_MaxRetriesExhausted(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":{"message":"bad gateway"}}`))
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
		retry:      RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	}

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("expected error to mention status code, got: %v", err)
	}
	// 1 initial + 3 retries = 4 total
	if callCount != 4 {
		t.Errorf("expected 4 calls (1 initial + 3 retries), got %d", callCount)
	}
}

func TestFireworksClient_Retry_ContextCancelled(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"error":{"message":"unavailable"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first response
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
		retry:      RetryConfig{MaxRetries: 10, BaseDelay: 200 * time.Millisecond, MaxDelay: 1 * time.Second},
	}

	_, err := client.ChatCompletion(ctx, ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Should not have completed all 10 retries
	if callCount > 5 {
		t.Errorf("expected early termination, but got %d calls", callCount)
	}
}

func TestFireworksClient_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(fireworksResponse{
			Error: &struct {
				Message string `json:"message"`
			}{Message: "invalid model"},
		})
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
		retry:      DefaultRetryConfig(),
	}

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "bad-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid model") {
		t.Errorf("expected error message, got: %v", err)
	}
}
