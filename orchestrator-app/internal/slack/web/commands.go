package web

import (
	"bytes"
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
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// ResponsePoster posts delayed responses to Slack response URLs
type ResponsePoster interface {
	PostResponse(ctx context.Context, responseURL string, payload map[string]interface{}) error
}

// SlashCommandHandler handles incoming Slack slash command requests
type SlashCommandHandler struct {
	threadFinder   ThreadFinder
	githubClient   GitHubCommenter
	issueCreator   GitHubIssueCreator
	slackClient    UserInfoResolver
	registry       TargetRegistry
	responsePoster ResponsePoster
	signingSecret  string
	slackWorkspace string
}

// NewSlashCommandHandler creates a new slash command handler
func NewSlashCommandHandler(
	threadFinder ThreadFinder,
	githubClient GitHubCommenter,
	issueCreator GitHubIssueCreator,
	slackClient UserInfoResolver,
	registry TargetRegistry,
	responsePoster ResponsePoster,
	signingSecret string,
	slackWorkspace string,
) *SlashCommandHandler {
	if responsePoster == nil {
		responsePoster = &defaultResponsePoster{}
	}
	return &SlashCommandHandler{
		threadFinder:   threadFinder,
		githubClient:   githubClient,
		issueCreator:   issueCreator,
		slackClient:    slackClient,
		registry:       registry,
		responsePoster: responsePoster,
		signingSecret:  signingSecret,
		slackWorkspace: slackWorkspace,
	}
}

// slashCommand represents the incoming Slack slash command payload
type slashCommand struct {
	Command     string `json:"command"`
	Text        string `json:"text"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	ThreadTs    string `json:"thread_ts"`
	ResponseURL string `json:"response_url"`
}

// HandleSlashCommand handles incoming Slack slash command requests
func (h *SlashCommandHandler) HandleSlashCommand(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read slash command body", "error", err)
		h.sendErrorResponse(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Verify Slack request signature
	if err := h.verifySignature(r, body); err != nil {
		slog.Warn("slash command signature verification failed", "error", err)
		h.sendErrorResponse(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse form-encoded body
	values, err := url.ParseQuery(string(body))
	if err != nil {
		slog.Error("failed to parse slash command body", "error", err)
		h.sendEphemeralResponse(w, "Error: Invalid request format")
		return
	}

	cmd := slashCommand{
		Command:     values.Get("command"),
		Text:        strings.TrimSpace(values.Get("text")),
		ChannelID:   values.Get("channel_id"),
		ChannelName: values.Get("channel_name"),
		UserID:      values.Get("user_id"),
		UserName:    values.Get("user_name"),
		ThreadTs:    values.Get("thread_ts"),
		ResponseURL: values.Get("response_url"),
	}

	switch cmd.Command {
	case "/create-issue":
		h.handleCreateIssue(w, cmd)
	case "/create-plan":
		h.handleCreatePlan(w, cmd)
	case "/implement":
		h.handleImplement(w, cmd)
	default:
		h.sendEphemeralResponse(w, fmt.Sprintf("Unknown command: %s", cmd.Command))
	}
}

// handleCreateIssue handles the /create-issue command
func (h *SlashCommandHandler) handleCreateIssue(w http.ResponseWriter, cmd slashCommand) {
	// Validate input
	if cmd.Text == "" {
		h.sendEphemeralResponse(w, "Error: Issue title is required. Usage: `/create-issue <title>`")
		return
	}

	// Resolve channel → target config
	target, ok := h.resolveTargetByChannel(context.Background(), cmd.ChannelID)
	if !ok {
		h.sendEphemeralResponse(w, "Error: This channel is not configured for GitHub integration.")
		return
	}

	// Send immediate ephemeral response
	h.sendEphemeralResponse(w, "⏳ Creating GitHub issue...")

	// Process asynchronously
	go func() {
		ctx := context.Background()

		// Resolve user display name
		displayName := cmd.UserName
		if name, err := h.slackClient.GetUserInfo(ctx, target.SlackBotToken, cmd.UserID); err == nil && name != "" {
			displayName = name
		}

		// Build Slack deep link (top-level message)
		slackLink := h.slackMessageLink(cmd.ChannelID, "", "", "")

		// Issue body with via-slack marker
		body := fmt.Sprintf("Requested by %s [via Slack](%s)\n\n<!-- via-slack -->", displayName, slackLink)

		// Create GitHub issue
		number, err := h.issueCreator.CreateIssue(ctx, target.RepoOwner, target.RepoName, cmd.Text, body, target.GitHubPAT)
		if err != nil {
			slog.Error("failed to create github issue from slash command", "error", err, "repo", target.RepoOwner+"/"+target.RepoName)
			h.postResponse(cmd.ResponseURL, map[string]interface{}{
				"response_type": "in_channel",
				"text":          fmt.Sprintf("❌ Failed to create GitHub issue: %v", err),
			})
			return
		}

		slog.Info("created github issue from slash command",
			"repo", target.RepoOwner+"/"+target.RepoName,
			"number", number,
			"title", cmd.Text,
			"user", displayName,
		)

		// Public confirmation in channel
		issueURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d", target.RepoOwner, target.RepoName, number)
		h.postResponse(cmd.ResponseURL, map[string]interface{}{
			"response_type": "in_channel",
			"text":          fmt.Sprintf("✅ Issue <%s|#%d> created: %s", issueURL, number, cmd.Text),
		})
	}()
}

// handleCreatePlan handles the /create-plan command (thread context only)
func (h *SlashCommandHandler) handleCreatePlan(w http.ResponseWriter, cmd slashCommand) {
	// Validate: must be in a thread
	if cmd.ThreadTs == "" {
		h.sendEphemeralResponse(w, "Error: This command must be used in a thread. Please reply in an existing issue/PR thread.")
		return
	}

	// Look up the thread
	ctx := context.Background()
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, cmd.ThreadTs)
	if err != nil {
		h.sendEphemeralResponse(w, "Error: This thread is not tracked. Make sure you're in an issue/PR thread.")
		return
	}

	// Look up target config
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		h.sendEphemeralResponse(w, "Error: Repository configuration not found.")
		return
	}

	// Send immediate ephemeral response
	h.sendEphemeralResponse(w, "⏳ Requesting implementation plan from OpenCode...")

	// Process asynchronously
	go func() {
		ctx := context.Background()

		// Build the /opencode comment
		comment := "/opencode Please write an implementation plan for this."
		if cmd.Text != "" {
			comment += " " + cmd.Text
		}

		// Determine GitHub number: PR if set, else issue
		number := thread.GithubIssueID
		if thread.GithubPRID > 0 {
			number = thread.GithubPRID
		}

		// Post to GitHub
		if _, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, target.GitHubPAT); err != nil {
			slog.Error("failed to post github comment from slash command", "error", err, "repo", thread.RepoOwner+"/"+thread.RepoName, "number", number)
			h.postResponse(cmd.ResponseURL, map[string]interface{}{
				"response_type": "in_channel",
				"text":          fmt.Sprintf("❌ Failed to request implementation plan: %v", err),
			})
			return
		}

		slog.Info("requested implementation plan from slash command",
			"repo", thread.RepoOwner+"/"+thread.RepoName,
			"number", number,
			"user", cmd.UserName,
		)

		// Public confirmation in thread
		h.postResponse(cmd.ResponseURL, map[string]interface{}{
			"response_type": "in_channel",
			"text":          "✅ Implementation plan requested. OpenCode will respond shortly.",
		})
	}()
}

// handleImplement handles the /implement command (thread context only)
func (h *SlashCommandHandler) handleImplement(w http.ResponseWriter, cmd slashCommand) {
	// Validate: must be in a thread
	if cmd.ThreadTs == "" {
		h.sendEphemeralResponse(w, "Error: This command must be used in a thread. Please reply in an existing issue/PR thread.")
		return
	}

	// Look up the thread
	ctx := context.Background()
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, cmd.ThreadTs)
	if err != nil {
		h.sendEphemeralResponse(w, "Error: This thread is not tracked. Make sure you're in an issue/PR thread.")
		return
	}

	// Look up target config
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		h.sendEphemeralResponse(w, "Error: Repository configuration not found.")
		return
	}

	// Send immediate ephemeral response
	h.sendEphemeralResponse(w, "⏳ Requesting implementation from OpenCode...")

	// Process asynchronously
	go func() {
		ctx := context.Background()

		// Build the /opencode comment
		comment := "/opencode Please implement the plan."
		if cmd.Text != "" {
			comment += " " + cmd.Text
		}

		// Determine GitHub number: PR if set, else issue
		number := thread.GithubIssueID
		if thread.GithubPRID > 0 {
			number = thread.GithubPRID
		}

		// Post to GitHub
		if _, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, target.GitHubPAT); err != nil {
			slog.Error("failed to post github comment from slash command", "error", err, "repo", thread.RepoOwner+"/"+thread.RepoName, "number", number)
			h.postResponse(cmd.ResponseURL, map[string]interface{}{
				"response_type": "in_channel",
				"text":          fmt.Sprintf("❌ Failed to request implementation: %v", err),
			})
			return
		}

		slog.Info("requested implementation from slash command",
			"repo", thread.RepoOwner+"/"+thread.RepoName,
			"number", number,
			"user", cmd.UserName,
		)

		// Public confirmation in thread
		h.postResponse(cmd.ResponseURL, map[string]interface{}{
			"response_type": "in_channel",
			"text":          "✅ Implementation requested. OpenCode will work on it and open a PR.",
		})
	}()
}

// resolveTargetByChannel resolves a Slack channel ID to a target config
func (h *SlashCommandHandler) resolveTargetByChannel(ctx context.Context, channelID string) (targets.TargetConfig, bool) {
	channelName, err := h.resolveChannelName(ctx, channelID)
	if err != nil {
		slog.Debug("failed to resolve channel name", "channel", channelID, "error", err)
		return targets.TargetConfig{}, false
	}

	return h.registry.GetByChannelName(channelName)
}

// resolveChannelName uses the first available bot token to resolve a channel ID to its name
func (h *SlashCommandHandler) resolveChannelName(ctx context.Context, channelID string) (string, error) {
	token := h.registry.AnyBotToken()
	if token == "" {
		return "", fmt.Errorf("no bot token available to resolve channel name")
	}
	return h.slackClient.GetChannelName(ctx, token, channelID)
}

// slackMessageLink builds a deep link to a Slack message
func (h *SlashCommandHandler) slackMessageLink(channel, ts, threadTs, cid string) string {
	slackHost := "slack.com"
	if h.slackWorkspace != "" {
		slackHost = h.slackWorkspace + ".slack.com"
	}

	// For top-level channel messages, we don't have a specific timestamp
	// Just link to the channel
	if ts == "" {
		return fmt.Sprintf("https://%s/archives/%s", slackHost, channel)
	}

	link := fmt.Sprintf("https://%s/archives/%s/p%s",
		slackHost, channel, strings.ReplaceAll(ts, ".", ""))
	if threadTs != "" {
		link += fmt.Sprintf("?thread_ts=%s&cid=%s", threadTs, cid)
	}
	return link
}

// sendEphemeralResponse sends an immediate ephemeral JSON response
func (h *SlashCommandHandler) sendEphemeralResponse(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"response_type": "ephemeral",
		"text":          text,
	})
}

// sendErrorResponse sends an error HTTP response
func (h *SlashCommandHandler) sendErrorResponse(w http.ResponseWriter, text string, status int) {
	w.WriteHeader(status)
	w.Write([]byte(text))
}

// postResponse posts a delayed response to the Slack response URL
func (h *SlashCommandHandler) postResponse(responseURL string, payload map[string]interface{}) {
	if responseURL == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.responsePoster.PostResponse(ctx, responseURL, payload); err != nil {
		slog.Error("failed to post slack response", "error", err, "url", responseURL)
	}
}

// verifySignature validates the Slack request signature using HMAC-SHA256
func (h *SlashCommandHandler) verifySignature(r *http.Request, body []byte) error {
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

// defaultResponsePoster is the default implementation of ResponsePoster
type defaultResponsePoster struct{}

// PostResponse posts a payload to a Slack response URL
func (p *defaultResponsePoster) PostResponse(ctx context.Context, responseURL string, payload map[string]interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", responseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("post response: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
