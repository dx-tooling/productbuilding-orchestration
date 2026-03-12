package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// ThreadFinder looks up a Slack thread by its timestamp
type ThreadFinder interface {
	FindThreadBySlackTs(ctx context.Context, threadTs string) (*domain.SlackThread, error)
}

// GitHubCommenter posts comments on GitHub issues/PRs
type GitHubCommenter interface {
	CreateComment(ctx context.Context, owner, repo string, number int, body, pat string) (int64, error)
}

// UserInfoResolver resolves a Slack user ID to a display name
type UserInfoResolver interface {
	GetUserInfo(ctx context.Context, botToken, userID string) (string, error)
}

// TargetRegistry looks up target configuration by repo
type TargetRegistry interface {
	Get(repoOwner, repoName string) (targets.TargetConfig, bool)
}

// Handler handles Slack Events API callbacks
type Handler struct {
	threadFinder   ThreadFinder
	githubClient   GitHubCommenter
	slackClient    UserInfoResolver
	registry       TargetRegistry
	signingSecret  string
}

// NewHandler creates a new Slack event handler
func NewHandler(
	threadFinder ThreadFinder,
	githubClient GitHubCommenter,
	slackClient UserInfoResolver,
	registry TargetRegistry,
	signingSecret string,
) *Handler {
	return &Handler{
		threadFinder:  threadFinder,
		githubClient:  githubClient,
		slackClient:   slackClient,
		registry:      registry,
		signingSecret: signingSecret,
	}
}

// slackEnvelope represents the outer Slack Events API payload
type slackEnvelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	Event     json.RawMessage `json:"event"`
	// authorizations is an array; we only need the first entry's user_id
	Authorizations []struct {
		UserID string `json:"user_id"`
	} `json:"authorizations"`
}

// slackAppMentionEvent represents the inner event for app_mention
type slackAppMentionEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Text     string `json:"text"`
	ThreadTs string `json:"thread_ts"`
	Channel  string `json:"channel"`
	Ts       string `json:"ts"`
}

var botMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

// HandleEvent handles incoming Slack Events API requests
func (h *Handler) HandleEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read slack event body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify Slack request signature
	if err := h.verifySignature(r, body); err != nil {
		slog.Warn("slack signature verification failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var envelope slackEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		slog.Error("failed to parse slack event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	switch envelope.Type {
	case "url_verification":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"challenge": envelope.Challenge})

	case "event_callback":
		var event slackAppMentionEvent
		if err := json.Unmarshal(envelope.Event, &event); err != nil {
			slog.Error("failed to parse app_mention event", "error", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		if event.Type == "app_mention" {
			var botUserID string
			if len(envelope.Authorizations) > 0 {
				botUserID = envelope.Authorizations[0].UserID
			}
			go h.handleAppMention(context.Background(), event, botUserID)
		}

		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

// handleAppMention processes an app_mention event and posts to GitHub if appropriate
func (h *Handler) handleAppMention(ctx context.Context, event slackAppMentionEvent, botUserID string) {
	// Ignore mentions not in a thread
	if event.ThreadTs == "" {
		slog.Debug("ignoring app_mention without thread_ts")
		return
	}

	// Look up the thread by Slack timestamp
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, event.ThreadTs)
	if err != nil {
		slog.Debug("ignoring app_mention in untracked thread", "thread_ts", event.ThreadTs, "error", err)
		return
	}

	// Look up target config for this repo
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		slog.Warn("no target config for tracked thread", "repo", thread.RepoOwner+"/"+thread.RepoName)
		return
	}

	// Resolve Slack user display name
	displayName := event.User
	if name, err := h.slackClient.GetUserInfo(ctx, target.SlackBotToken, event.User); err == nil && name != "" {
		displayName = name
	}

	// Strip bot mention from text
	text := event.Text
	if botUserID != "" {
		text = strings.ReplaceAll(text, "<@"+botUserID+">", "")
	} else {
		text = botMentionRe.ReplaceAllString(text, "")
	}
	text = strings.TrimSpace(text)

	// Determine GitHub number: PR if set, else issue
	number := thread.GithubIssueID
	if thread.GithubPRID > 0 {
		number = thread.GithubPRID
	}

	// Format the comment
	comment := fmt.Sprintf("**%s** via Slack:\n\n%s\n\n<!-- via-slack -->", displayName, text)

	// Post to GitHub
	if _, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, target.GitHubPAT); err != nil {
		slog.Error("failed to post github comment from slack", "error", err, "repo", thread.RepoOwner+"/"+thread.RepoName, "number", number)
		return
	}

	slog.Info("posted github comment from slack",
		"repo", thread.RepoOwner+"/"+thread.RepoName,
		"number", number,
		"user", displayName,
	)
}

// verifySignature validates the Slack request signature using HMAC-SHA256
func (h *Handler) verifySignature(r *http.Request, body []byte) error {
	signature := r.Header.Get("X-Slack-Signature")
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")

	if signature == "" || timestamp == "" {
		return fmt.Errorf("missing signature or timestamp headers")
	}

	// Reject stale timestamps (> 5 minutes old)
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("timestamp too old")
	}

	// Compute expected signature
	sigBase := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write([]byte(sigBase))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}
