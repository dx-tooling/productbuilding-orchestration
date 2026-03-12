package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
	Title     string
	Body      string
	Author    string
}

// webhookPayload mirrors the relevant fields of a GitHub PR webhook.
type webhookPayload struct {
	Action      string `json:"action"`
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
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
		Title:     p.PullRequest.Title,
		Body:      p.PullRequest.Body,
		Author:    p.PullRequest.User.Login,
	}, nil
}

// IssueEvent represents a parsed GitHub issue webhook event.
type IssueEvent struct {
	Action     string     `json:"action"`
	Issue      Issue      `json:"issue"`
	Repository Repository `json:"repository"`
}

// Issue represents a GitHub issue
type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	User   User   `json:"user"`
}

// IssueCommentEvent represents a parsed GitHub issue comment webhook event.
type IssueCommentEvent struct {
	Action     string     `json:"action"`
	Comment    Comment    `json:"comment"`
	Issue      Issue      `json:"issue"`
	Repository Repository `json:"repository"`
}

// Comment represents a GitHub comment
type Comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
	User User   `json:"user"`
}

// Repository represents a GitHub repository
type Repository struct {
	Owner User   `json:"owner"`
	Name  string `json:"name"`
}

// User represents a GitHub user
type User struct {
	Login string `json:"login"`
}

// issuePayload mirrors the relevant fields of a GitHub issue webhook.
type issuePayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"issue"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// issueCommentPayload mirrors the relevant fields of a GitHub issue comment webhook.
type issueCommentPayload struct {
	Action  string `json:"action"`
	Comment struct {
		ID   int64  `json:"id"`
		Body string `json:"body"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	} `json:"comment"`
	Issue struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"issue"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// ParseIssueEvent extracts an IssueEvent from a raw webhook payload.
func ParseIssueEvent(payload []byte) (*IssueEvent, error) {
	var p issuePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("unmarshal issue payload: %w", err)
	}

	if p.Issue.Number == 0 {
		return nil, fmt.Errorf("not an issue event")
	}

	return &IssueEvent{
		Action: p.Action,
		Issue: Issue{
			Number: p.Issue.Number,
			Title:  p.Issue.Title,
			Body:   p.Issue.Body,
			User:   User{Login: p.Issue.User.Login},
		},
		Repository: Repository{
			Owner: User{Login: p.Repository.Owner.Login},
			Name:  p.Repository.Name,
		},
	}, nil
}

// ParseIssueCommentEvent extracts an IssueCommentEvent from a raw webhook payload.
func ParseIssueCommentEvent(payload []byte) (*IssueCommentEvent, error) {
	var p issueCommentPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("unmarshal issue comment payload: %w", err)
	}

	return &IssueCommentEvent{
		Action: p.Action,
		Comment: Comment{
			ID:   p.Comment.ID,
			Body: p.Comment.Body,
			User: User{Login: p.Comment.User.Login},
		},
		Issue: Issue{
			Number: p.Issue.Number,
			Title:  p.Issue.Title,
		},
		Repository: Repository{
			Owner: User{Login: p.Repository.Owner.Login},
			Name:  p.Repository.Name,
		},
	}, nil
}

// closingKeywordRe matches GitHub closing keywords: Fixes #N, Closes #N, Resolves #N (and variants)
var closingKeywordRe = regexp.MustCompile(`(?i)\b(?:fix(?:e[sd])?|close[sd]?|resolve[sd]?)\s+#(\d+)\b`)

// ExtractLinkedIssue parses a PR body for GitHub closing keywords (e.g. "Fixes #16")
// and returns the first linked issue number, or 0 if none found.
func ExtractLinkedIssue(body string) int {
	match := closingKeywordRe.FindStringSubmatch(body)
	if match == nil {
		return 0
	}
	n, err := strconv.Atoi(match[1])
	if err != nil {
		return 0
	}
	return n
}
