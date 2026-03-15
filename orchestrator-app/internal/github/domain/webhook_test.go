package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestValidateSignature(t *testing.T) {
	secret := "test-secret"
	payload := []byte(`{"action":"opened"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	t.Run("valid signature", func(t *testing.T) {
		if err := ValidateSignature(payload, validSig, secret); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("invalid signature", func(t *testing.T) {
		if err := ValidateSignature(payload, "sha256=0000000000000000000000000000000000000000000000000000000000000000", secret); err == nil {
			t.Error("expected error for invalid signature")
		}
	})

	t.Run("missing prefix", func(t *testing.T) {
		if err := ValidateSignature(payload, "invalid", secret); err == nil {
			t.Error("expected error for missing prefix")
		}
	})
}

func TestParsePREvent(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"pull_request": {
			"number": 42,
			"head": {
				"sha": "abc123",
				"ref": "feature/test"
			}
		},
		"repository": {
			"owner": {"login": "example-org"},
			"name": "my-app"
		}
	}`)

	event, err := ParsePREvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Action != "opened" {
		t.Errorf("Action = %q, want %q", event.Action, "opened")
	}
	if event.RepoOwner != "example-org" {
		t.Errorf("RepoOwner = %q, want %q", event.RepoOwner, "example-org")
	}
	if event.RepoName != "my-app" {
		t.Errorf("RepoName = %q, want %q", event.RepoName, "my-app")
	}
	if event.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want %d", event.PRNumber, 42)
	}
	if event.Branch != "feature/test" {
		t.Errorf("Branch = %q, want %q", event.Branch, "feature/test")
	}
	if event.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want %q", event.HeadSHA, "abc123")
	}
}

func TestParsePREvent_NotPR(t *testing.T) {
	payload := []byte(`{"action": "created", "issue": {"number": 1}}`)
	_, err := ParsePREvent(payload)
	if err == nil {
		t.Error("expected error for non-PR event")
	}
}

func TestParsePREvent_ExtractsBodyTitleAuthor(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"pull_request": {
			"number": 17,
			"title": "Added tech/arch section to homepage",
			"body": "Fixes #16\n\nAdded technical architecture section",
			"user": {"login": "opencode-agent[bot]"},
			"head": {
				"sha": "c07b81d7",
				"ref": "feature/homepage-tech"
			}
		},
		"repository": {
			"owner": {"login": "example-org"},
			"name": "playground"
		}
	}`)

	event, err := ParsePREvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if event.Title != "Added tech/arch section to homepage" {
		t.Errorf("Title = %q, want %q", event.Title, "Added tech/arch section to homepage")
	}
	if event.Body != "Fixes #16\n\nAdded technical architecture section" {
		t.Errorf("Body = %q, want body with 'Fixes #16'", event.Body)
	}
	if event.Author != "opencode-agent[bot]" {
		t.Errorf("Author = %q, want %q", event.Author, "opencode-agent[bot]")
	}
}

func TestExtractLinkedIssue(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{"Fixes #N", "Fixes #16", 16},
		{"fixes lowercase", "fixes #42", 42},
		{"Closes #N", "Closes #99", 99},
		{"closes lowercase", "closes #7", 7},
		{"Resolves #N", "Resolves #123", 123},
		{"Fix #N", "Fix #5", 5},
		{"Close #N", "Close #10", 10},
		{"Resolve #N", "Resolve #3", 3},
		{"Fixed #N", "Fixed #8", 8},
		{"Closed #N", "Closed #11", 11},
		{"Resolved #N", "Resolved #12", 12},
		{"in longer text", "This PR implements the feature.\n\nFixes #16\n\nAdded technical architecture section", 16},
		{"no linked issue", "Just a regular PR body without any closing references", 0},
		{"empty body", "", 0},
		{"hash without keyword falls back to ref", "See issue #16 for details", 16},
		{"implements ref", "Implements #51 - forgot password feature", 51},
		{"bare ref in body", "Created from #42", 42},
		{"closing keyword preferred over bare ref", "See #99 for context. Fixes #10", 10},
		{"multiple - takes first", "Fixes #10 and Closes #20", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinkedIssue(tt.body)
			if got != tt.want {
				t.Errorf("ExtractLinkedIssue(%q) = %d, want %d", tt.body, got, tt.want)
			}
		})
	}
}
