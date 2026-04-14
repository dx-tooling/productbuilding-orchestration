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

func TestParsePREvent_Merged(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {
			"number": 42,
			"merged": true,
			"title": "Add feature",
			"user": {"login": "alice"},
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

	if !event.Merged {
		t.Error("Expected Merged to be true for merged PR")
	}
}

func TestParsePREvent_ClosedNotMerged(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"pull_request": {
			"number": 42,
			"merged": false,
			"title": "Abandoned PR",
			"user": {"login": "alice"},
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

	if event.Merged {
		t.Error("Expected Merged to be false for closed-not-merged PR")
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

func TestParseCheckRunEvent_Failure(t *testing.T) {
	payload := []byte(`{
		"action": "completed",
		"check_run": {
			"id": 1001,
			"name": "build",
			"status": "completed",
			"conclusion": "failure",
			"html_url": "https://github.com/acme/widgets/runs/1001",
			"head_sha": "abc123",
			"pull_requests": [{"number": 10}]
		},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseCheckRunEvent(payload)
	if err != nil {
		t.Fatalf("ParseCheckRunEvent() error = %v", err)
	}

	if event.Action != "completed" {
		t.Errorf("Action = %q, want %q", event.Action, "completed")
	}
	if event.CheckRun.Name != "build" {
		t.Errorf("Name = %q, want %q", event.CheckRun.Name, "build")
	}
	if event.CheckRun.Conclusion != "failure" {
		t.Errorf("Conclusion = %q, want %q", event.CheckRun.Conclusion, "failure")
	}
	if event.CheckRun.HeadSHA != "abc123" {
		t.Errorf("HeadSHA = %q, want %q", event.CheckRun.HeadSHA, "abc123")
	}
	if len(event.CheckRun.PullRequests) != 1 {
		t.Fatalf("PullRequests len = %d, want 1", len(event.CheckRun.PullRequests))
	}
	if event.CheckRun.PullRequests[0].Number != 10 {
		t.Errorf("PR number = %d, want 10", event.CheckRun.PullRequests[0].Number)
	}
	if event.Repository.Owner.Login != "acme" {
		t.Errorf("Owner = %q, want %q", event.Repository.Owner.Login, "acme")
	}
}

func TestParsePREvent_ParsesSender(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"pull_request": {
			"number": 42,
			"title": "Add feature",
			"user": {"login": "alice"},
			"head": {"sha": "abc123", "ref": "feature/test"}
		},
		"sender": {"login": "alice"},
		"repository": {
			"owner": {"login": "example-org"},
			"name": "my-app"
		}
	}`)

	event, err := ParsePREvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Sender != "alice" {
		t.Errorf("Sender = %q, want %q", event.Sender, "alice")
	}
}

func TestParsePREvent_SenderAbsent(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"pull_request": {
			"number": 42,
			"title": "Add feature",
			"user": {"login": "alice"},
			"head": {"sha": "abc123", "ref": "feature/test"}
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
	if event.Sender != "" {
		t.Errorf("Sender = %q, want empty string when sender absent", event.Sender)
	}
}

func TestParseIssueEvent_ParsesSender(t *testing.T) {
	payload := []byte(`{
		"action": "closed",
		"issue": {
			"number": 7,
			"title": "Bug report",
			"body": "Steps to reproduce...",
			"user": {"login": "alice"}
		},
		"sender": {"login": "PrdctBldr"},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseIssueEvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Sender != "PrdctBldr" {
		t.Errorf("Sender = %q, want %q", event.Sender, "PrdctBldr")
	}
}

func TestParseIssueEvent_SenderAbsent(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"issue": {
			"number": 7,
			"title": "Bug report",
			"body": "",
			"user": {"login": "alice"}
		},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseIssueEvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Sender != "" {
		t.Errorf("Sender = %q, want empty string when sender absent", event.Sender)
	}
}

func TestParseIssueCommentEvent_ParsesSender(t *testing.T) {
	payload := []byte(`{
		"action": "created",
		"comment": {
			"id": 100,
			"body": "Looks good",
			"user": {"login": "bob"}
		},
		"issue": {"number": 5, "title": "Feature"},
		"sender": {"login": "bob"},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseIssueCommentEvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Sender != "bob" {
		t.Errorf("Sender = %q, want %q", event.Sender, "bob")
	}
}

func TestParseCheckRunEvent_ParsesSender(t *testing.T) {
	payload := []byte(`{
		"action": "completed",
		"check_run": {
			"id": 1001,
			"name": "build",
			"status": "completed",
			"conclusion": "failure",
			"html_url": "https://github.com/acme/widgets/runs/1001",
			"head_sha": "abc123",
			"pull_requests": [{"number": 10}]
		},
		"sender": {"login": "github-actions[bot]"},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseCheckRunEvent(payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Sender != "github-actions[bot]" {
		t.Errorf("Sender = %q, want %q", event.Sender, "github-actions[bot]")
	}
}

func TestParseCheckRunEvent_Success(t *testing.T) {
	payload := []byte(`{
		"action": "completed",
		"check_run": {
			"id": 1002,
			"name": "lint",
			"status": "completed",
			"conclusion": "success",
			"html_url": "https://github.com/acme/widgets/runs/1002",
			"head_sha": "def456",
			"pull_requests": [{"number": 10}]
		},
		"repository": {
			"owner": {"login": "acme"},
			"name": "widgets"
		}
	}`)

	event, err := ParseCheckRunEvent(payload)
	if err != nil {
		t.Fatalf("ParseCheckRunEvent() error = %v", err)
	}

	if event.CheckRun.Conclusion != "success" {
		t.Errorf("Conclusion = %q, want %q", event.CheckRun.Conclusion, "success")
	}
	if event.CheckRun.HTMLURL != "https://github.com/acme/widgets/runs/1002" {
		t.Errorf("HTMLURL = %q", event.CheckRun.HTMLURL)
	}
}
