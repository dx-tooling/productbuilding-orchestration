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

	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// ModalOpener opens Slack modals via views.open API
type ModalOpener interface {
	OpenView(ctx context.Context, botToken, triggerID string, view map[string]interface{}) error
}

// InteractionsHandler handles Slack interaction payloads (shortcuts, modal submissions)
type InteractionsHandler struct {
	threadFinder   ThreadFinder
	githubClient   GitHubCommenter
	issueCreator   GitHubIssueCreator
	slackClient    UserInfoResolver
	threadPoster   ThreadPoster
	registry       TargetRegistry
	responsePoster ResponsePoster
	modalOpener    ModalOpener
	signingSecret  string
	slackWorkspace string
}

// NewInteractionsHandler creates a new interactions handler
func NewInteractionsHandler(
	threadFinder ThreadFinder,
	githubClient GitHubCommenter,
	issueCreator GitHubIssueCreator,
	slackClient UserInfoResolver,
	threadPoster ThreadPoster,
	registry TargetRegistry,
	responsePoster ResponsePoster,
	modalOpener ModalOpener,
	signingSecret string,
	slackWorkspace string,
) *InteractionsHandler {
	if responsePoster == nil {
		responsePoster = &defaultResponsePoster{}
	}
	if modalOpener == nil {
		modalOpener = &defaultModalOpener{}
	}
	return &InteractionsHandler{
		threadFinder:   threadFinder,
		githubClient:   githubClient,
		issueCreator:   issueCreator,
		slackClient:    slackClient,
		threadPoster:   threadPoster,
		registry:       registry,
		responsePoster: responsePoster,
		modalOpener:    modalOpener,
		signingSecret:  signingSecret,
		slackWorkspace: slackWorkspace,
	}
}

// interactionPayload represents a Slack interaction payload (can be shortcut or view_submission)
type interactionPayload struct {
	Type        string      `json:"type"`
	CallbackID  string      `json:"callback_id"`
	TriggerID   string      `json:"trigger_id"`
	Channel     channelInfo `json:"channel"`
	Message     messageInfo `json:"message"`
	MessageTs   string      `json:"message_ts"`
	User        userInfo    `json:"user"`
	ResponseURL string      `json:"response_url"`
	View        viewInfo    `json:"view"`
}

type channelInfo struct {
	ID string `json:"id"`
}

type messageInfo struct {
	Type     string `json:"type"`
	Ts       string `json:"ts"`
	ThreadTs string `json:"thread_ts"`
	Text     string `json:"text"`
	User     string `json:"user"`
}

type userInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type viewInfo struct {
	CallbackID      string                 `json:"callback_id"`
	PrivateMetadata string                 `json:"private_metadata"`
	State           map[string]interface{} `json:"state"`
}

// HandleInteractions handles incoming Slack interaction requests
func (h *InteractionsHandler) HandleInteractions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read interaction body", "error", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Verify Slack request signature
	if err := h.verifySignature(r, body); err != nil {
		slog.Warn("interaction signature verification failed", "error", err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse payload (can be JSON or form-encoded)
	var payload interactionPayload
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "application/x-www-form-urlencoded") {
		// Parse form data
		values, err := url.ParseQuery(string(body))
		if err != nil {
			slog.Error("failed to parse interaction form data", "error", err)
			h.sendJSONResponse(w, map[string]interface{}{
				"error": "Invalid request format",
			}, http.StatusBadRequest)
			return
		}

		payloadStr := values.Get("payload")
		if payloadStr == "" {
			slog.Error("missing payload in form data")
			h.sendJSONResponse(w, map[string]interface{}{
				"error": "Missing payload",
			}, http.StatusBadRequest)
			return
		}

		if err := json.Unmarshal([]byte(payloadStr), &payload); err != nil {
			slog.Error("failed to parse interaction payload", "error", err)
			h.sendJSONResponse(w, map[string]interface{}{
				"error": "Invalid payload",
			}, http.StatusBadRequest)
			return
		}
	} else {
		// Parse JSON directly
		if err := json.Unmarshal(body, &payload); err != nil {
			slog.Error("failed to parse interaction payload", "error", err)
			h.sendJSONResponse(w, map[string]interface{}{
				"error": "Invalid payload",
			}, http.StatusBadRequest)
			return
		}
	}

	// Route based on payload type
	// Note: Slack sends message shortcuts as "message_action", not "shortcut"
	switch payload.Type {
	case "shortcut", "message_action":
		h.handleShortcut(w, payload)
	case "view_submission":
		h.handleViewSubmission(w, payload)
	default:
		slog.Warn("unknown interaction type", "type", payload.Type)
		// Acknowledge but do nothing
		w.WriteHeader(http.StatusOK)
	}
}

// handleShortcut processes shortcut interactions
func (h *InteractionsHandler) handleShortcut(w http.ResponseWriter, payload interactionPayload) {
	// Always acknowledge immediately (required by Slack within 3 seconds)
	w.WriteHeader(http.StatusOK)

	// For message shortcuts in threads, we need the parent thread_ts, not the individual message ts
	// If the shortcut was clicked on a threaded message, use thread_ts (parent), otherwise use message ts
	threadTs := payload.Message.ThreadTs
	if threadTs == "" {
		threadTs = payload.Message.Ts
	}

	// Debug logging
	slog.Info("shortcut received", "callback_id", payload.CallbackID, "thread_ts", threadTs, "message_ts", payload.Message.Ts, "user", payload.User.Name)

	switch payload.CallbackID {
	case "create_plan":
		h.handleCreatePlanShortcut(payload, threadTs)
	case "implement":
		h.handleImplementShortcut(payload, threadTs)
	case "add_comment":
		h.handleAddCommentShortcut(payload, threadTs)
	default:
		slog.Warn("unknown shortcut callback_id", "callback_id", payload.CallbackID)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": fmt.Sprintf("Unknown shortcut: %s", payload.CallbackID),
		})
	}
}

// handleCreatePlanShortcut processes the "Create implementation plan" shortcut
func (h *InteractionsHandler) handleCreatePlanShortcut(payload interactionPayload, threadTs string) {
	// Look up the thread using the parent thread timestamp
	ctx := context.Background()
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, threadTs)
	if err != nil {
		slog.Debug("shortcut used in untracked thread", "thread_ts", threadTs)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "This message is not tracked by ProductBuilder. Please use this shortcut on a ProductBuilder message in an issue/PR thread.",
		})
		return
	}

	// Look up target config
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		slog.Warn("no target config for tracked thread", "repo", thread.RepoOwner+"/"+thread.RepoName)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "Repository configuration not found.",
		})
		return
	}

	// Build the /opencode comment
	comment := "/opencode Please write an implementation plan for this."

	// Determine GitHub number: PR if set, else issue
	number := thread.GithubIssueID
	if thread.GithubPRID > 0 {
		number = thread.GithubPRID
	}

	// Post to GitHub
	if _, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, target.GitHubPAT); err != nil {
		slog.Error("failed to post github comment from shortcut", "error", err, "repo", thread.RepoOwner+"/"+thread.RepoName, "number", number)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": fmt.Sprintf("❌ Failed to request implementation plan: %v", err),
		})
		return
	}

	slog.Info("requested implementation plan from shortcut",
		"repo", thread.RepoOwner+"/"+thread.RepoName,
		"number", number,
		"user", payload.User.Name,
	)

	// Public confirmation in thread
	h.postResponse(payload.ResponseURL, map[string]interface{}{
		"text": "✅ Implementation plan requested. OpenCode will respond shortly.",
	})
}

// handleImplementShortcut processes the "Implement this" shortcut
func (h *InteractionsHandler) handleImplementShortcut(payload interactionPayload, threadTs string) {
	// Look up the thread using the parent thread timestamp
	ctx := context.Background()
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, threadTs)
	if err != nil {
		slog.Debug("shortcut used in untracked thread", "thread_ts", threadTs)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "This message is not tracked by ProductBuilder. Please use this shortcut on a ProductBuilder message in an issue/PR thread.",
		})
		return
	}

	// Look up target config
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		slog.Warn("no target config for tracked thread", "repo", thread.RepoOwner+"/"+thread.RepoName)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "Repository configuration not found.",
		})
		return
	}

	// Build the /opencode comment
	comment := "/opencode Please implement the plan."

	// Determine GitHub number: PR if set, else issue
	number := thread.GithubIssueID
	if thread.GithubPRID > 0 {
		number = thread.GithubPRID
	}

	// Post to GitHub
	if _, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, target.GitHubPAT); err != nil {
		slog.Error("failed to post github comment from shortcut", "error", err, "repo", thread.RepoOwner+"/"+thread.RepoName, "number", number)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": fmt.Sprintf("❌ Failed to request implementation: %v", err),
		})
		return
	}

	slog.Info("requested implementation from shortcut",
		"repo", thread.RepoOwner+"/"+thread.RepoName,
		"number", number,
		"user", payload.User.Name,
	)

	// Public confirmation in thread
	h.postResponse(payload.ResponseURL, map[string]interface{}{
		"text": "✅ Implementation requested. OpenCode will work on it and open a PR.",
	})
}

// handleAddCommentShortcut opens a modal for the user to enter a comment
func (h *InteractionsHandler) handleAddCommentShortcut(payload interactionPayload, threadTs string) {
	// Look up the thread to ensure it's tracked
	ctx := context.Background()
	thread, err := h.threadFinder.FindThreadBySlackTs(ctx, threadTs)
	if err != nil {
		slog.Debug("shortcut used in untracked thread", "thread_ts", threadTs)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "This message is not tracked by ProductBuilder. Please use this shortcut on a ProductBuilder message in an issue/PR thread.",
		})
		return
	}

	// Look up target config
	target, ok := h.registry.Get(thread.RepoOwner, thread.RepoName)
	if !ok {
		slog.Warn("no target config for tracked thread", "repo", thread.RepoOwner+"/"+thread.RepoName)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "Repository configuration not found.",
		})
		return
	}

	// Store thread info in private_metadata for later retrieval
	privateMeta := map[string]string{
		"thread_ts":       threadTs,
		"channel":         payload.Channel.ID,
		"repo_owner":      thread.RepoOwner,
		"repo_name":       thread.RepoName,
		"github_issue_id": fmt.Sprintf("%d", thread.GithubIssueID),
		"github_pr_id":    fmt.Sprintf("%d", thread.GithubPRID),
		"user_id":         payload.User.ID,
		"bot_token":       target.SlackBotToken,
		"github_pat":      target.GitHubPAT,
	}
	privateMetaJSON, _ := json.Marshal(privateMeta)

	// Build modal view
	modal := map[string]interface{}{
		"type":        "modal",
		"callback_id": "add_comment_modal",
		"title": map[string]string{
			"type": "plain_text",
			"text": "Add GitHub Comment",
		},
		"submit": map[string]string{
			"type": "plain_text",
			"text": "Post Comment",
		},
		"close": map[string]string{
			"type": "plain_text",
			"text": "Cancel",
		},
		"private_metadata": string(privateMetaJSON),
		"blocks": []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": fmt.Sprintf("Add a comment to the GitHub issue/PR for *%s/%s*", thread.RepoOwner, thread.RepoName),
				},
			},
			{
				"type":     "input",
				"block_id": "comment_block",
				"element": map[string]interface{}{
					"type":      "plain_text_input",
					"action_id": "comment_input",
					"multiline": true,
					"placeholder": map[string]string{
						"type": "plain_text",
						"text": "Enter your comment here...",
					},
				},
				"label": map[string]string{
					"type": "plain_text",
					"text": "Comment",
				},
			},
		},
	}

	// Open the modal
	if err := h.modalOpener.OpenView(ctx, target.SlackBotToken, payload.TriggerID, modal); err != nil {
		slog.Error("failed to open modal", "error", err)
		h.postResponse(payload.ResponseURL, map[string]interface{}{
			"text": "Failed to open comment dialog. Please try again.",
		})
		return
	}

	slog.Info("opened add comment modal",
		"user", payload.User.Name,
		"repo", thread.RepoOwner+"/"+thread.RepoName,
	)
}

// handleViewSubmission processes modal submissions
func (h *InteractionsHandler) handleViewSubmission(w http.ResponseWriter, payload interactionPayload) {
	if payload.View.CallbackID != "add_comment_modal" {
		// Not our modal - just acknowledge
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse private_metadata
	var privateMeta map[string]string
	if err := json.Unmarshal([]byte(payload.View.PrivateMetadata), &privateMeta); err != nil {
		slog.Error("failed to parse private_metadata", "error", err)
		// Return error to Slack modal
		h.sendJSONResponse(w, map[string]interface{}{
			"response_action": "errors",
			"errors": map[string]string{
				"comment_block": "Internal error. Please try again.",
			},
		}, http.StatusOK)
		return
	}

	// Extract comment text from modal state
	commentText := h.extractCommentFromState(payload.View.State)
	if commentText == "" {
		// Return validation error to modal
		h.sendJSONResponse(w, map[string]interface{}{
			"response_action": "errors",
			"errors": map[string]string{
				"comment_block": "Please enter a comment",
			},
		}, http.StatusOK)
		return
	}

	// Acknowledge modal submission (close the modal)
	w.WriteHeader(http.StatusOK)

	// Process asynchronously
	go func() {
		ctx := context.Background()

		// Get thread info
		thread, err := h.threadFinder.FindThreadBySlackTs(ctx, privateMeta["thread_ts"])
		if err != nil {
			slog.Error("thread not found when processing modal submission", "error", err)
			return
		}

		// Get user display name
		userID := privateMeta["user_id"]
		displayName := userID
		botToken := privateMeta["bot_token"]
		if botToken != "" {
			if name, err := h.slackClient.GetUserInfo(ctx, botToken, userID); err == nil && name != "" {
				displayName = name
			}
		}

		// Determine GitHub number
		number := thread.GithubIssueID
		if thread.GithubPRID > 0 {
			number = thread.GithubPRID
		}

		// Build Slack deep link
		slackLink := h.slackMessageLink(privateMeta["channel"], "", "", "")

		// Build comment body with attribution
		comment := fmt.Sprintf("**%s** [via Slack](%s):\n\n%s\n\n<!-- via-slack -->",
			displayName, slackLink, commentText)

		// Post to GitHub
		githubPAT := privateMeta["github_pat"]
		commentID, err := h.githubClient.CreateComment(ctx, thread.RepoOwner, thread.RepoName, number, comment, githubPAT)
		if err != nil {
			slog.Error("failed to post github comment from modal", "error", err)
			return
		}

		// Construct GitHub comment URL
		githubURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d#issuecomment-%d",
			thread.RepoOwner, thread.RepoName, number, commentID)

		// Truncate comment for display (250 chars max)
		displayComment := commentText
		if len(displayComment) > 250 {
			displayComment = displayComment[:250] + "..."
		}

		// Format confirmation message
		confirmationMsg := fmt.Sprintf("✅ **%s** posted a comment: \"%s\" <%s|View on GitHub>",
			displayName, displayComment, githubURL)

		// Post confirmation to Slack thread
		if h.threadPoster != nil {
			msg := domain.MessageBlock{Text: confirmationMsg}
			if err := h.threadPoster.PostToThread(ctx, botToken, privateMeta["channel"], privateMeta["thread_ts"], msg); err != nil {
				slog.Error("failed to post confirmation to slack thread", "error", err)
				// Don't fail - GitHub comment was already posted successfully
			}
		}

		slog.Info("posted github comment from modal",
			"repo", thread.RepoOwner+"/"+thread.RepoName,
			"number", number,
			"user", displayName,
		)
	}()
}

// extractCommentFromState extracts the comment text from modal state
func (h *InteractionsHandler) extractCommentFromState(state map[string]interface{}) string {
	if state == nil {
		return ""
	}

	values, ok := state["values"].(map[string]interface{})
	if !ok {
		return ""
	}

	commentBlock, ok := values["comment_block"].(map[string]interface{})
	if !ok {
		return ""
	}

	commentInput, ok := commentBlock["comment_input"].(map[string]interface{})
	if !ok {
		return ""
	}

	commentText, ok := commentInput["value"].(string)
	if !ok {
		return ""
	}

	return commentText
}

// slackMessageLink builds a deep link to a Slack message
func (h *InteractionsHandler) slackMessageLink(channel, ts, threadTs, cid string) string {
	slackHost := "slack.com"
	if h.slackWorkspace != "" {
		slackHost = h.slackWorkspace + ".slack.com"
	}

	// For messages, link to channel
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

// sendJSONResponse sends a JSON response
func (h *InteractionsHandler) sendJSONResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// postResponse posts a delayed response to the Slack response URL
func (h *InteractionsHandler) postResponse(responseURL string, payload map[string]interface{}) {
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
func (h *InteractionsHandler) verifySignature(r *http.Request, body []byte) error {
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

// defaultModalOpener is the default implementation of ModalOpener
type defaultModalOpener struct{}

// OpenView opens a modal view via Slack API
func (m *defaultModalOpener) OpenView(ctx context.Context, botToken, triggerID string, view map[string]interface{}) error {
	payload := map[string]interface{}{
		"trigger_id": triggerID,
		"view":       view,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/views.open", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+botToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack api returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Check Slack API response
	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &slackResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !slackResp.OK {
		return fmt.Errorf("slack api error: %s", slackResp.Error)
	}

	return nil
}
