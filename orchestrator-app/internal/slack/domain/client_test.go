package domain

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_PostMessage(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != "POST" {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat.postMessage" {
			t.Errorf("Expected path /chat.postMessage, got %s", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Expected Authorization header Bearer test-token, got %s", auth)
		}

		// Parse request body
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse request body: %v", err)
			return
		}

		// Verify channel
		if req["channel"] != "#test-channel" {
			t.Errorf("Expected channel #test-channel, got %v", req["channel"])
		}

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"ts": "1234567890.123456",
		})
	}))
	defer server.Close()

	// Create client with test server
	client := NewClientWithBaseURL("test-token", server.URL)

	msg := MessageBlock{
		Text: "Test message",
	}

	ts, err := client.PostMessage(context.Background(), "#test-channel", msg)
	if err != nil {
		t.Errorf("PostMessage() error = %v", err)
		return
	}

	if ts != "1234567890.123456" {
		t.Errorf("PostMessage() timestamp = %v, want 1234567890.123456", ts)
	}
}

func TestClient_PostToThread(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		// Verify thread_ts is set
		if req["thread_ts"] != "parent-ts-123" {
			t.Errorf("Expected thread_ts parent-ts-123, got %v", req["thread_ts"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	msg := MessageBlock{
		Text: "Reply in thread",
	}

	err := client.PostToThread(context.Background(), "#test-channel", "parent-ts-123", msg)
	if err != nil {
		t.Errorf("PostToThread() error = %v", err)
	}
}

func TestClient_AddReaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reactions.add" {
			t.Errorf("Expected path /reactions.add, got %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)

		if req["name"] != "white_check_mark" {
			t.Errorf("Expected emoji name white_check_mark, got %v", req["name"])
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	err := client.AddReaction(context.Background(), "#test-channel", "msg-ts-123", "white_check_mark")
	if err != nil {
		t.Errorf("AddReaction() error = %v", err)
	}
}

func TestClient_RemoveReaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/reactions.remove" {
			t.Errorf("Expected path /reactions.remove, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	err := client.RemoveReaction(context.Background(), "#test-channel", "msg-ts-123", "arrows_counterclockwise")
	if err != nil {
		t.Errorf("RemoveReaction() error = %v", err)
	}
}

func TestClient_PostMessage_SlackError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"error": "channel_not_found",
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	msg := MessageBlock{Text: "Test"}
	_, err := client.PostMessage(context.Background(), "#invalid", msg)
	if err == nil {
		t.Error("PostMessage() expected error for failed Slack response")
	}
}

func TestClient_PostMessage_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)

	msg := MessageBlock{Text: "Test"}
	_, err := client.PostMessage(context.Background(), "#test", msg)
	if err == nil {
		t.Error("PostMessage() expected error for HTTP 500")
	}
}
