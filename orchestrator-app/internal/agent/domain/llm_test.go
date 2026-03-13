package domain

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestFireworksClient_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer server.Close()

	client := &FireworksClient{
		httpClient: server.Client(),
		apiKey:     "test-key",
		baseURL:    server.URL,
	}

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("expected error to mention status code, got: %v", err)
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
