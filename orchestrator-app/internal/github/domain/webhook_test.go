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
			"owner": {"login": "luminor-project"},
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
	if event.RepoOwner != "luminor-project" {
		t.Errorf("RepoOwner = %q, want %q", event.RepoOwner, "luminor-project")
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
