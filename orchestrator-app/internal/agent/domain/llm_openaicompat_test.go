package domain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenAICompatClient_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody)

		if reqBody["model"] != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "Hello!"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClient("test-key", "gpt-4o", server.URL)
	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", resp.FinishReason)
	}
}

func TestOpenAICompatClient_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]any{
									"name":      "create_github_issue",
									"arguments": `{"title":"Bug fix"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClient("test-key", "gpt-4o", server.URL)
	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Create an issue"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_123" {
		t.Errorf("expected tool ID 'call_123', got %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Function.Name != "create_github_issue" {
		t.Errorf("expected tool name 'create_github_issue', got %q", resp.ToolCalls[0].Function.Name)
	}
}

func TestOpenAICompatClient_SystemMessage(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClient("test-key", "gpt-4o", server.URL)
	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// System message should be passed through as-is in OpenAI format
	messages := capturedBody["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}
	sysMsg := messages[0].(map[string]any)
	if sysMsg["role"] != "system" {
		t.Errorf("expected system role, got %v", sysMsg["role"])
	}
}

func TestOpenAICompatClient_ToolResultRoundTrip(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "Done"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClient("test-key", "gpt-4o", server.URL)
	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Create issue"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{ID: "call_1", Type: "function", Function: FunctionCall{Name: "create_github_issue", Arguments: `{"title":"Bug"}`}},
				},
			},
			{Role: "tool", Content: `{"number":42}`, ToolCallID: "call_1"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	messages := capturedBody["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// Verify tool result message
	toolMsg := messages[2].(map[string]any)
	if toolMsg["role"] != "tool" {
		t.Errorf("expected role 'tool', got %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("expected tool_call_id 'call_1', got %v", toolMsg["tool_call_id"])
	}
}

func TestOpenAICompatClient_NonRetryableError(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "bad request", "type": "invalid_request_error"},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClientWithConfig("test-key", "gpt-4o", server.URL,
		RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (no retries for 400), got %d", callCount)
	}
}

func TestOpenAICompatClient_RetryThenSuccess(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "rate limited"},
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{
					"message":       map[string]any{"role": "assistant", "content": "Success"},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClientWithConfig("test-key", "gpt-4o", server.URL,
		RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Success" {
		t.Errorf("expected 'Success', got %q", resp.Content)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestOpenAICompatClient_MaxRetriesExhausted_ProviderUnavailable(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "bad gateway"},
		})
	}))
	defer server.Close()

	client := NewOpenAICompatClientWithConfig("test-key", "gpt-4o", server.URL,
		RetryConfig{MaxRetries: 2, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
	)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (1 initial + 2 retries), got %d", callCount)
	}
	var unavail *ProviderUnavailableError
	if !errors.As(err, &unavail) {
		t.Errorf("expected ProviderUnavailableError, got %T: %v", err, err)
	}
	if unavail.Provider != "openaicompat" {
		t.Errorf("expected provider 'openaicompat', got %q", unavail.Provider)
	}
}

func TestOpenAICompatClient_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{}})
	}))
	defer server.Close()

	client := NewOpenAICompatClient("test-key", "gpt-4o", server.URL)
	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}
