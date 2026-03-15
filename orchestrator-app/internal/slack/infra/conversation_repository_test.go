package infra

import (
	"context"
	"testing"
	"time"

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
)

func TestConversationRepository_UpsertAndList(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewConversationRepository(db)
	ctx := context.Background()

	// Insert two conversations
	conv1 := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "1111111111.111111",
		Summary:      "Implement sign in feature",
		UserName:     "alice",
		LastActiveAt: time.Now().Add(-time.Hour),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}
	conv2 := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "2222222222.222222",
		Summary:      "Fix sign up bug",
		UserName:     "bob",
		LastActiveAt: time.Now(),
		LinkedIssue:  42,
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}

	if err := repo.UpsertConversation(ctx, conv1); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	if err := repo.UpsertConversation(ctx, conv2); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// List conversations
	convs, err := repo.ListRecentConversations(ctx, "C123", 14)
	if err != nil {
		t.Fatalf("ListRecentConversations() error = %v", err)
	}

	if len(convs) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(convs))
	}

	// Should be ordered by most recent first
	if convs[0].Summary != "Fix sign up bug" {
		t.Errorf("expected first conv to be most recent, got %q", convs[0].Summary)
	}
	if convs[1].Summary != "Implement sign in feature" {
		t.Errorf("expected second conv to be older, got %q", convs[1].Summary)
	}
	if convs[0].LinkedIssue != 42 {
		t.Errorf("expected linked issue 42, got %d", convs[0].LinkedIssue)
	}
}

func TestConversationRepository_UpsertUpdatesExisting(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewConversationRepository(db)
	ctx := context.Background()

	// Insert initial conversation
	conv := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "1111111111.111111",
		Summary:      "Initial summary",
		UserName:     "alice",
		LastActiveAt: time.Now().Add(-time.Hour),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}
	if err := repo.UpsertConversation(ctx, conv); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// Upsert with updated summary and later timestamp
	conv.Summary = "Updated summary"
	conv.LastActiveAt = time.Now()
	if err := repo.UpsertConversation(ctx, conv); err != nil {
		t.Fatalf("UpsertConversation() second call error = %v", err)
	}

	convs, err := repo.ListRecentConversations(ctx, "C123", 14)
	if err != nil {
		t.Fatalf("ListRecentConversations() error = %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation after upsert, got %d", len(convs))
	}
	if convs[0].Summary != "Updated summary" {
		t.Errorf("expected updated summary, got %q", convs[0].Summary)
	}
}

func TestConversationRepository_UpsertPreservesLinkedIssue(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewConversationRepository(db)
	ctx := context.Background()

	// Insert with linked issue
	conv := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "1111111111.111111",
		Summary:      "Feature discussion",
		UserName:     "alice",
		LastActiveAt: time.Now().Add(-time.Hour),
		LinkedIssue:  42,
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}
	if err := repo.UpsertConversation(ctx, conv); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// Upsert without linked issue — should preserve the existing one
	conv.LinkedIssue = 0
	conv.LastActiveAt = time.Now()
	if err := repo.UpsertConversation(ctx, conv); err != nil {
		t.Fatalf("UpsertConversation() second call error = %v", err)
	}

	convs, err := repo.ListRecentConversations(ctx, "C123", 14)
	if err != nil {
		t.Fatalf("ListRecentConversations() error = %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(convs))
	}
	if convs[0].LinkedIssue != 42 {
		t.Errorf("expected linked issue 42 preserved, got %d", convs[0].LinkedIssue)
	}
}

func TestConversationRepository_ListRespectsDateCutoff(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewConversationRepository(db)
	ctx := context.Background()

	// Insert one recent and one old conversation
	recent := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "1111111111.111111",
		Summary:      "Recent",
		UserName:     "alice",
		LastActiveAt: time.Now(),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}
	old := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "0000000000.000000",
		Summary:      "Old",
		UserName:     "bob",
		LastActiveAt: time.Now().AddDate(0, 0, -30),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}

	if err := repo.UpsertConversation(ctx, recent); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	if err := repo.UpsertConversation(ctx, old); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// Only recent should appear with 14-day window
	convs, err := repo.ListRecentConversations(ctx, "C123", 14)
	if err != nil {
		t.Fatalf("ListRecentConversations() error = %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation within 14 days, got %d", len(convs))
	}
	if convs[0].Summary != "Recent" {
		t.Errorf("expected recent conversation, got %q", convs[0].Summary)
	}
}

func TestConversationRepository_ListDifferentChannels(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := NewConversationRepository(db)
	ctx := context.Background()

	conv1 := agent.Conversation{
		ChannelID:    "C123",
		ThreadTs:     "1111111111.111111",
		Summary:      "Channel 1 conv",
		UserName:     "alice",
		LastActiveAt: time.Now(),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}
	conv2 := agent.Conversation{
		ChannelID:    "C456",
		ThreadTs:     "2222222222.222222",
		Summary:      "Channel 2 conv",
		UserName:     "bob",
		LastActiveAt: time.Now(),
		RepoOwner:    "acme",
		RepoName:     "widgets",
	}

	if err := repo.UpsertConversation(ctx, conv1); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}
	if err := repo.UpsertConversation(ctx, conv2); err != nil {
		t.Fatalf("UpsertConversation() error = %v", err)
	}

	// Should only return conversations for the specified channel
	convs, err := repo.ListRecentConversations(ctx, "C123", 14)
	if err != nil {
		t.Fatalf("ListRecentConversations() error = %v", err)
	}

	if len(convs) != 1 {
		t.Fatalf("expected 1 conversation for C123, got %d", len(convs))
	}
	if convs[0].Summary != "Channel 1 conv" {
		t.Errorf("expected Channel 1 conv, got %q", convs[0].Summary)
	}
}
