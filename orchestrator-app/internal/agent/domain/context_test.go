package domain

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"hello", 1},       // 5/4 = 1
		{"hello world", 2}, // 11/4 = 2
		{strings.Repeat("a", 100), 25},
		{strings.Repeat("a", 4), 1},
	}

	for _, tt := range tests {
		got := EstimateTokens(tt.text)
		if got != tt.expected {
			t.Errorf("EstimateTokens(%q) = %d, want %d", tt.text[:min(len(tt.text), 20)], got, tt.expected)
		}
	}
}

func TestBuildContext_BasicAssembly(t *testing.T) {
	budget := TokenBudget{Total: 8000, IssueMaxTokens: 1000, ThreadMaxMessages: 20}

	msgs := BuildContext(
		"You are a helpful agent.",
		"alice says: hello",
		nil,
		nil,
		budget,
	)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected first message to be system, got %s", msgs[0].Role)
	}
	if msgs[1].Role != "user" {
		t.Errorf("expected second message to be user, got %s", msgs[1].Role)
	}
}

func TestBuildContext_ThreadTruncation(t *testing.T) {
	budget := TokenBudget{Total: 8000, IssueMaxTokens: 1000, ThreadMaxMessages: 5}

	// Create 50 thread messages
	thread := make([]ThreadMessage, 50)
	for i := range thread {
		thread[i] = ThreadMessage{User: "U001", Text: "message " + strings.Repeat("x", 10)}
	}

	msgs := BuildContext(
		"system prompt",
		"user message",
		thread,
		nil,
		budget,
	)

	// Should have: system + 5 most recent thread messages + user = 7
	threadMsgCount := 0
	for _, m := range msgs {
		if m.Role == "user" && m.Content != "user message" {
			threadMsgCount++
		}
		// Thread messages from non-bot users get role "user"
	}

	// Total messages should not exceed system + ThreadMaxMessages + user
	if len(msgs) > 1+budget.ThreadMaxMessages+1 {
		t.Errorf("expected at most %d messages, got %d", 1+budget.ThreadMaxMessages+1, len(msgs))
	}
}

func TestBuildContext_ThreadKeepsMostRecent(t *testing.T) {
	budget := TokenBudget{Total: 8000, IssueMaxTokens: 1000, ThreadMaxMessages: 3}

	thread := []ThreadMessage{
		{User: "U001", Text: "oldest message", Ts: "100.001"},
		{User: "U002", Text: "middle message", Ts: "100.002", BotID: "B001"},
		{User: "U001", Text: "recent message", Ts: "100.003"},
		{User: "U002", Text: "newest message", Ts: "100.004", BotID: "B001"},
	}

	msgs := BuildContext(
		"system",
		"current question",
		thread,
		nil,
		budget,
	)

	// Should drop "oldest message", keep the 3 most recent
	hasOldest := false
	hasNewest := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "oldest message") {
			hasOldest = true
		}
		if strings.Contains(m.Content, "newest message") {
			hasNewest = true
		}
	}

	if hasOldest {
		t.Error("expected oldest message to be dropped")
	}
	if !hasNewest {
		t.Error("expected newest message to be kept")
	}
}

func TestBuildContext_IssueBodyTruncated(t *testing.T) {
	budget := TokenBudget{Total: 8000, IssueMaxTokens: 50, ThreadMaxMessages: 20}

	// Create a very long issue body (~1000 tokens)
	longBody := strings.Repeat("word ", 1000) // ~5000 chars = ~1250 tokens

	issue := &IssueContext{
		Number: 42,
		Title:  "Test",
		Body:   longBody,
		State:  "open",
	}

	msgs := BuildContext(
		"system",
		"user msg",
		nil,
		issue,
		budget,
	)

	// Find the issue context message
	var issueMsg *Message
	for i := range msgs {
		if msgs[i].Role == "system" && strings.Contains(msgs[i].Content, "#42") {
			issueMsg = &msgs[i]
			break
		}
	}

	if issueMsg == nil {
		t.Fatal("expected issue context message")
	}

	// The issue message should be truncated
	issueTokens := EstimateTokens(issueMsg.Content)
	// Allow some overhead for the context prefix text
	if issueTokens > budget.IssueMaxTokens+50 {
		t.Errorf("expected issue tokens <= %d (+overhead), got %d", budget.IssueMaxTokens, issueTokens)
	}
}

func TestBuildContext_PriorityUnderTightBudget(t *testing.T) {
	// Very tight budget: only enough for system prompt + user message
	budget := TokenBudget{Total: 20, IssueMaxTokens: 5, ThreadMaxMessages: 20}

	thread := []ThreadMessage{
		{User: "U001", Text: "thread message " + strings.Repeat("x", 100)},
	}

	issue := &IssueContext{
		Number: 42,
		Title:  "Test",
		Body:   strings.Repeat("body ", 100),
		State:  "open",
	}

	msgs := BuildContext(
		"sys",
		"hi",
		thread,
		issue,
		budget,
	)

	// System and user messages must always be present
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages (system + user), got %d", len(msgs))
	}
	if msgs[0].Role != "system" && msgs[0].Content != "sys" {
		t.Error("expected system message first")
	}
	if msgs[len(msgs)-1].Role != "user" {
		t.Error("expected user message last")
	}
}

func TestBuildContext_BotMessagesGetAssistantRole(t *testing.T) {
	budget := TokenBudget{Total: 8000, IssueMaxTokens: 1000, ThreadMaxMessages: 20}

	thread := []ThreadMessage{
		{User: "U001", Text: "user msg", Ts: "100.001"},
		{User: "U002", Text: "bot reply", Ts: "100.002", BotID: "B001"},
	}

	msgs := BuildContext(
		"system",
		"current",
		thread,
		nil,
		budget,
	)

	// Find the bot message
	foundAssistant := false
	for _, m := range msgs {
		if m.Content == "bot reply" && m.Role == "assistant" {
			foundAssistant = true
		}
	}

	if !foundAssistant {
		t.Error("expected bot thread message to have role 'assistant'")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
