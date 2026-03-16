package domain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnthropicClient_TextResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify the request body has correct Anthropic format
		body, _ := io.ReadAll(r.Body)
		var reqBody map[string]any
		json.Unmarshal(body, &reqBody)

		if reqBody["model"] != "claude-opus-4-6-20250616" {
			t.Errorf("expected model claude-opus-4-6-20250616, got %v", reqBody["model"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_test123",
			"type":  "message",
			"role":  "assistant",
			"model": "claude-opus-4-6-20250616",
			"content": []map[string]any{
				{"type": "text", "text": "Hello, world!"},
			},
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		})
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key", RetryConfig{}, server.URL)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-opus-4-6-20250616",
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

func TestAnthropicClient_SystemMessageExtracted(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "test",
			"content":     []map[string]any{{"type": "text", "text": "ok"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key", RetryConfig{}, server.URL)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model: "test",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hi"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// System message should be extracted to the system field
	systemBlocks, ok := capturedBody["system"].([]any)
	if !ok || len(systemBlocks) == 0 {
		t.Fatalf("expected system field with blocks, got %v", capturedBody["system"])
	}
	block := systemBlocks[0].(map[string]any)
	if block["text"] != "You are a helpful assistant." {
		t.Errorf("expected system text, got %v", block["text"])
	}

	// Messages should not contain the system message
	messages := capturedBody["messages"].([]any)
	for _, m := range messages {
		msg := m.(map[string]any)
		if msg["role"] == "system" {
			t.Error("system message should not be in messages array")
		}
	}
}

func TestAnthropicClient_ToolCallResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":    "msg_test",
			"type":  "message",
			"role":  "assistant",
			"model": "test",
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_123",
					"name":  "create_github_issue",
					"input": map[string]any{"title": "Bug fix", "body": "Fix the login"},
				},
			},
			"stop_reason": "tool_use",
			"usage":       map[string]any{"input_tokens": 10, "output_tokens": 15},
		})
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key", RetryConfig{}, server.URL)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "Create an issue"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_123" {
		t.Errorf("expected tool ID 'toolu_123', got %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Type != "function" {
		t.Errorf("expected tool type 'function', got %q", resp.ToolCalls[0].Type)
	}
	if resp.ToolCalls[0].Function.Name != "create_github_issue" {
		t.Errorf("expected tool name 'create_github_issue', got %q", resp.ToolCalls[0].Function.Name)
	}

	var args map[string]any
	json.Unmarshal([]byte(resp.ToolCalls[0].Function.Arguments), &args)
	if args["title"] != "Bug fix" {
		t.Errorf("expected title 'Bug fix', got %v", args["title"])
	}
	if resp.FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason 'tool_calls', got %q", resp.FinishReason)
	}
}

func TestAnthropicClient_ToolUseRoundTrip(t *testing.T) {
	var capturedBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "test",
			"content":     []map[string]any{{"type": "text", "text": "Done"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key", RetryConfig{}, server.URL)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model: "test",
		Messages: []Message{
			{Role: "user", Content: "Create an issue"},
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID:   "toolu_123",
						Type: "function",
						Function: FunctionCall{
							Name:      "create_github_issue",
							Arguments: `{"title":"Bug"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				Content:    `{"number":42}`,
				ToolCallID: "toolu_123",
			},
		},
		Tools: []ToolDef{
			{
				Type: "function",
				Function: ToolSchema{
					Name:        "create_github_issue",
					Description: "Create a GitHub issue",
					Parameters:  json.RawMessage(`{"type":"object","properties":{"title":{"type":"string"}}}`),
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the request has correct Anthropic format
	messages := capturedBody["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}

	// First message: user
	userMsg := messages[0].(map[string]any)
	if userMsg["role"] != "user" {
		t.Errorf("expected first message role 'user', got %v", userMsg["role"])
	}

	// Second message: assistant with tool_use content block
	assistantMsg := messages[1].(map[string]any)
	if assistantMsg["role"] != "assistant" {
		t.Errorf("expected second message role 'assistant', got %v", assistantMsg["role"])
	}
	assistantContent := assistantMsg["content"].([]any)
	toolUseBlock := assistantContent[0].(map[string]any)
	if toolUseBlock["type"] != "tool_use" {
		t.Errorf("expected tool_use block, got %v", toolUseBlock["type"])
	}
	if toolUseBlock["id"] != "toolu_123" {
		t.Errorf("expected tool use id 'toolu_123', got %v", toolUseBlock["id"])
	}
	if toolUseBlock["name"] != "create_github_issue" {
		t.Errorf("expected tool name, got %v", toolUseBlock["name"])
	}

	// Third message: user with tool_result content block
	toolResultMsg := messages[2].(map[string]any)
	if toolResultMsg["role"] != "user" {
		t.Errorf("expected third message role 'user', got %v", toolResultMsg["role"])
	}
	toolResultContent := toolResultMsg["content"].([]any)
	toolResultBlock := toolResultContent[0].(map[string]any)
	if toolResultBlock["type"] != "tool_result" {
		t.Errorf("expected tool_result block, got %v", toolResultBlock["type"])
	}
	if toolResultBlock["tool_use_id"] != "toolu_123" {
		t.Errorf("expected tool_use_id 'toolu_123', got %v", toolResultBlock["tool_use_id"])
	}

	// Verify tools are in Anthropic format
	tools := capturedBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "create_github_issue" {
		t.Errorf("expected tool name, got %v", tool["name"])
	}
}

func TestAnthropicClient_HTTPError_NonRetryable(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`))
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key",
		RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
		server.URL,
	)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 call (no retries for 400), got %d", callCount)
	}
}

func TestAnthropicClient_Retry_429ThenSuccess(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		if callCount <= 2 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":          "msg_test",
			"type":        "message",
			"role":        "assistant",
			"model":       "test",
			"content":     []map[string]any{{"type": "text", "text": "Success after retries"}},
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key",
		RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
		server.URL,
	)

	resp, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
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

func TestAnthropicClient_Retry_MaxRetriesExhausted(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"bad gateway"}}`))
	}))
	defer server.Close()

	client := NewAnthropicClientWithConfig("test-key",
		RetryConfig{MaxRetries: 3, BaseDelay: time.Millisecond, MaxDelay: 10 * time.Millisecond},
		server.URL,
	)

	_, err := client.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "Hi"}},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// 1 initial + 3 retries = 4 total
	if callCount != 4 {
		t.Errorf("expected 4 calls (1 initial + 3 retries), got %d", callCount)
	}
}

func TestAnthropicClient_Retry_ContextCancelled(t *testing.T) {
	var callCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":"unavailable"}}`))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	client := NewAnthropicClientWithConfig("test-key",
		RetryConfig{MaxRetries: 10, BaseDelay: 200 * time.Millisecond, MaxDelay: 1 * time.Second},
		server.URL,
	)

	_, err := client.ChatCompletion(ctx, ChatRequest{
		Model:    "test",
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
