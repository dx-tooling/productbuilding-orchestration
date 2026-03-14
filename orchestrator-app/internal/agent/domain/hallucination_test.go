package domain

import "testing"

func TestDetectHallucination(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		effects SideEffects
		wantHit bool // true = non-empty correction expected
	}{
		// --- Should detect hallucinations ---
		{
			name:    "delegation claim with empty effects",
			text:    "I've asked OpenCode to create a plan on issue #42.",
			effects: SideEffects{},
			wantHit: true,
		},
		{
			name: "delegation claim with comments but no delegated issues",
			text: "I've delegated this to OpenCode for implementation.",
			effects: SideEffects{
				PostedComments: []int64{111},
			},
			wantHit: true,
		},
		{
			name:    "issue creation claim with empty effects",
			text:    "I created issue #99 to track this bug.",
			effects: SideEffects{},
			wantHit: true,
		},
		{
			name:    "comment posting claim with empty effects",
			text:    "I've posted a comment on the issue with the details.",
			effects: SideEffects{},
			wantHit: true,
		},
		{
			name:    "lowercase variant of delegation",
			text:    "i triggered opencode to work on the implementation",
			effects: SideEffects{},
			wantHit: true,
		},

		// --- Should pass (no hallucination) ---
		{
			name: "delegation claim backed by delegated issues",
			text: "I've asked OpenCode to create a plan on issue #42.",
			effects: SideEffects{
				DelegatedIssues: []int{42},
			},
			wantHit: false,
		},
		{
			name: "issue creation claim backed by created issues",
			text: "I created issue #99 for the feature request.",
			effects: SideEffects{
				CreatedIssues: []CreatedIssue{{Number: 99, Title: "Feature"}},
			},
			wantHit: false,
		},
		{
			name: "comment claim backed by posted comments",
			text: "I've posted a comment with the migration plan.",
			effects: SideEffects{
				PostedComments: []int64{555},
			},
			wantHit: false,
		},
		{
			name:    "benign response - help offer",
			text:    "I can help you with that.",
			effects: SideEffects{},
			wantHit: false,
		},
		{
			name:    "benign response - search results",
			text:    "I found 3 open issues related to authentication.",
			effects: SideEffects{},
			wantHit: false,
		},
		{
			name:    "future tense - not a claim of completed action",
			text:    "To create an issue, I'll need more details about the bug.",
			effects: SideEffects{},
			wantHit: false,
		},
		{
			name:    "empty response text",
			text:    "",
			effects: SideEffects{},
			wantHit: false,
		},
		{
			name:    "opened issue pattern with empty effects",
			text:    "I opened issue #7 for this.",
			effects: SideEffects{},
			wantHit: true,
		},
		{
			name:    "asked opencode to pattern",
			text:    "I asked OpenCode to implement this feature.",
			effects: SideEffects{},
			wantHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correction := DetectHallucination(tt.text, tt.effects)
			if tt.wantHit && correction == "" {
				t.Error("expected hallucination to be detected, but got empty correction")
			}
			if !tt.wantHit && correction != "" {
				t.Errorf("expected no hallucination, but got correction: %s", correction)
			}
		})
	}
}

func TestTruncateForLog(t *testing.T) {
	if got := truncateForLog("short", 10); got != "short" {
		t.Errorf("expected 'short', got %q", got)
	}
	if got := truncateForLog("a longer string here", 7); got != "a longe…" {
		t.Errorf("expected truncation, got %q", got)
	}
	if got := truncateForLog("", 5); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}
