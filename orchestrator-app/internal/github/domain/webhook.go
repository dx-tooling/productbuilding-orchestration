package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateSignature verifies the GitHub webhook HMAC-SHA256 signature.
func ValidateSignature(payload []byte, signature string, secret string) error {
	if !strings.HasPrefix(signature, "sha256=") {
		return fmt.Errorf("unsupported signature format")
	}

	expected := signature[7:]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actual := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(actual)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// PREvent represents a parsed GitHub pull request webhook event.
type PREvent struct {
	Action    string // opened, synchronize, reopened, closed
	RepoOwner string
	RepoName  string
	PRNumber  int
	Branch    string
	HeadSHA   string
}

// webhookPayload mirrors the relevant fields of a GitHub PR webhook.
type webhookPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// ParsePREvent extracts a PREvent from a raw webhook payload.
func ParsePREvent(payload []byte) (*PREvent, error) {
	var p webhookPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("unmarshal webhook payload: %w", err)
	}

	if p.PullRequest.Number == 0 {
		return nil, fmt.Errorf("not a pull request event")
	}

	return &PREvent{
		Action:    p.Action,
		RepoOwner: p.Repository.Owner.Login,
		RepoName:  p.Repository.Name,
		PRNumber:  p.PullRequest.Number,
		Branch:    p.PullRequest.Head.Ref,
		HeadSHA:   p.PullRequest.Head.SHA,
	}, nil
}
