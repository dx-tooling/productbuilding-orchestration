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

	agent "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/agent/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/featurecontext"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/domain"
)

// AgentRunner runs the LLM agent loop.
type AgentRunner interface {
	Run(ctx context.Context, req agent.RunRequest) (agent.RunResponse, error)
}

// ThreadFinder looks up a Slack thread by its timestamp.
type ThreadFinder interface {
	FindThreadBySlackTs(ctx context.Context, threadTs string) (*domain.SlackThread, error)
}

// ThreadSaver persists thread-to-issue mappings.
type ThreadSaver interface {
	SaveThread(ctx context.Context, thread *domain.SlackThread) error
}

// SlackClient combines user info, thread posting, and reaction management.
type SlackClient interface {
	GetUserInfo(ctx context.Context, botToken, userID string) (string, error)
	GetChannelName(ctx context.Context, botToken, channelID string) (string, error)
	PostMessage(ctx context.Context, botToken, channel string, msg domain.MessageBlock) (string, error)
	PostToThread(ctx context.Context, botToken, channel, threadTs string, msg domain.MessageBlock) error
	AddReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error
	RemoveReaction(ctx context.Context, botToken, channel, timestamp, emoji string) error
}

// TargetRegistry looks up target configuration by repo or channel name.
type TargetRegistry interface {
	Get(repoOwner, repoName string) (targets.TargetConfig, bool)
	GetByChannelName(channelName string) (targets.TargetConfig, bool)
	AnyBotToken() string
}

// ConversationRecorder persists conversation metadata after each agent response.
type ConversationRecorder interface {
	UpsertConversation(ctx context.Context, conv agent.Conversation) error
}

// TraceSaveRequest contains the data needed to persist an agent execution trace.
type TraceSaveRequest struct {
	RepoOwner     string
	RepoName      string
	GithubIssueID int
	SlackChannel  string
	SlackThreadTs string
	UserName      string
	UserText      string
	TraceData     string
	Error         string
}

// TraceSaver persists agent execution traces.
type TraceSaver interface {
	SaveTrace(ctx context.Context, record TraceSaveRequest) error
}

// FeatureContextAssembler fetches aggregated feature context for enriching agent requests.
type FeatureContextAssembler interface {
	ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*featurecontext.FeatureSnapshot, error)
	ForIssue(ctx context.Context, owner, repo, pat string, number int) (*featurecontext.FeatureSnapshot, error)
}

// Handler handles Slack Events API callbacks.
type Handler struct {
	agent                AgentRunner
	threadFinder         ThreadFinder
	threadSaver          ThreadSaver
	conversationRecorder ConversationRecorder
	slackClient          SlackClient
	registry             TargetRegistry
	traceSaver           TraceSaver
	featureAssembler     FeatureContextAssembler
	signingSecret        string
	slackWorkspace       string
	agentTimeout         time.Duration
}

// NewHandler creates a new Slack event handler.
func NewHandler(
	agentRunner AgentRunner,
	threadFinder ThreadFinder,
	threadSaver ThreadSaver,
	conversationRecorder ConversationRecorder,
	slackClient SlackClient,
	registry TargetRegistry,
	signingSecret string,
	slackWorkspace string,
) *Handler {
	return &Handler{
		agent:                agentRunner,
		threadFinder:         threadFinder,
		threadSaver:          threadSaver,
		conversationRecorder: conversationRecorder,
		slackClient:          slackClient,
		registry:             registry,
		signingSecret:        signingSecret,
		slackWorkspace:       slackWorkspace,
		agentTimeout:         120 * time.Second,
	}
}

// SetAgentTimeout overrides the default agent run timeout.
func (h *Handler) SetAgentTimeout(d time.Duration) {
	h.agentTimeout = d
}

// SetTraceSaver sets the trace persistence backend.
func (h *Handler) SetTraceSaver(ts TraceSaver) {
	h.traceSaver = ts
}

// SetFeatureAssembler sets the feature context assembler for enriching agent requests.
func (h *Handler) SetFeatureAssembler(fa FeatureContextAssembler) {
	h.featureAssembler = fa
}

// slackEnvelope represents the outer Slack Events API payload.
type slackEnvelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	Event     json.RawMessage `json:"event"`
	// authorizations is an array; we only need the first entry's user_id
	Authorizations []struct {
		UserID string `json:"user_id"`
	} `json:"authorizations"`
}

// slackAppMentionEvent represents the inner event for app_mention.
type slackAppMentionEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Text     string `json:"text"`
	ThreadTs string `json:"thread_ts"`
	Channel  string `json:"channel"`
	Ts       string `json:"ts"`
}

var botMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

// HandleEvent handles incoming Slack Events API requests.
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
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), h.agentTimeout)
				defer cancel()
				h.handleAppMention(ctx, event, botUserID)
			}()
		}

		w.WriteHeader(http.StatusOK)

	default:
		w.WriteHeader(http.StatusOK)
	}
}

// handleAppMention processes an app_mention event via the LLM agent.
func (h *Handler) handleAppMention(ctx context.Context, event slackAppMentionEvent, botUserID string) {
	// Resolve channel → target
	target, ok := h.resolveTargetByChannel(ctx, event.Channel)
	if !ok {
		slog.Debug("ignoring app_mention in unregistered channel", "channel", event.Channel)
		return
	}

	// Resolve user display name
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

	// Add :eyes: reaction as thinking indicator
	_ = h.slackClient.AddReaction(ctx, target.SlackBotToken, event.Channel, event.Ts, "eyes")

	// Look up existing thread mapping for linked issue context
	var linkedIssue *agent.IssueContext
	threadTs := event.ThreadTs
	if threadTs != "" {
		if thread, err := h.threadFinder.FindThreadBySlackTs(ctx, threadTs); err == nil && thread != nil {
			linkedIssue = &agent.IssueContext{
				Number: thread.GithubIssueID,
				Title:  "", // Will be fetched by agent if needed
				State:  "",
			}
			if thread.GithubPRID > 0 {
				linkedIssue.Number = thread.GithubPRID
			}
		}
	}

	// Assemble feature context for enriching agent with current state
	var featureSummary string
	if h.featureAssembler != nil && threadTs != "" {
		if thread, err := h.threadFinder.FindThreadBySlackTs(ctx, threadTs); err == nil && thread != nil {
			var snap *featurecontext.FeatureSnapshot
			if thread.GithubPRID > 0 {
				snap, _ = h.featureAssembler.ForPR(ctx, target.RepoOwner, target.RepoName, target.GitHubPAT, thread.GithubPRID, thread.GithubIssueID)
			} else if thread.GithubIssueID > 0 {
				snap, _ = h.featureAssembler.ForIssue(ctx, target.RepoOwner, target.RepoName, target.GitHubPAT, thread.GithubIssueID)
			}
			if snap != nil {
				featureSummary = FormatFeatureSummary(snap)
			}
		}
	}

	// Build agent request
	req := agent.RunRequest{
		ChannelID:      event.Channel,
		ThreadTs:       threadTs,
		MessageTs:      event.Ts,
		UserText:       text,
		UserName:       displayName,
		BotUserID:      botUserID,
		Target:         target,
		LinkedIssue:    linkedIssue,
		FeatureSummary: featureSummary,
	}

	// Attach trace to context for recording
	trace := &agent.Trace{}
	ctx = agent.WithTrace(ctx, trace)

	// Run agent
	resp, err := h.agent.Run(ctx, req)

	// Persist trace
	if h.traceSaver != nil {
		// Determine the issue ID: prefer linked issue, fall back to first created issue
		issueID := 0
		if linkedIssue != nil {
			issueID = linkedIssue.Number
		}
		if issueID == 0 && err == nil && len(resp.SideEffects.CreatedIssues) > 0 {
			issueID = resp.SideEffects.CreatedIssues[0].Number
		}
		if issueID == 0 && err == nil && len(resp.SideEffects.DelegatedIssues) > 0 {
			issueID = resp.SideEffects.DelegatedIssues[0]
		}

		// Use event.Ts as thread_ts for top-level mentions (it becomes the thread parent)
		threadTs := event.ThreadTs
		if threadTs == "" {
			threadTs = event.Ts
		}

		traceJSON, _ := json.Marshal(trace)
		traceErr := ""
		if err != nil {
			traceErr = err.Error()
		}
		if saveErr := h.traceSaver.SaveTrace(ctx, TraceSaveRequest{
			RepoOwner:     target.RepoOwner,
			RepoName:      target.RepoName,
			GithubIssueID: issueID,
			SlackChannel:  event.Channel,
			SlackThreadTs: threadTs,
			UserName:      displayName,
			UserText:      text,
			TraceData:     string(traceJSON),
			Error:         traceErr,
		}); saveErr != nil {
			slog.Error("failed to save trace", "error", saveErr)
		}
	}

	// Remove :eyes:, add :white_check_mark:
	_ = h.slackClient.RemoveReaction(ctx, target.SlackBotToken, event.Channel, event.Ts, "eyes")

	if err != nil {
		slog.Error("agent error", "error", err, "channel", event.Channel)
		_ = h.slackClient.AddReaction(ctx, target.SlackBotToken, event.Channel, event.Ts, "x")
		replyTs := event.Ts
		if event.ThreadTs != "" {
			replyTs = event.ThreadTs
		}
		if err := h.slackClient.PostToThread(ctx, target.SlackBotToken, event.Channel, replyTs,
			domain.MessageBlock{Text: userFacingErrorMessage(err)}); err != nil {
			slog.Error("failed to post error reply", "error", err, "channel", event.Channel, "thread_ts", replyTs)
		}
		return
	}

	_ = h.slackClient.AddReaction(ctx, target.SlackBotToken, event.Channel, event.Ts, "white_check_mark")

	// Post response as thread reply
	replyTs := event.Ts // top-level mention → start new thread
	if event.ThreadTs != "" {
		replyTs = event.ThreadTs // in-thread mention → reply in existing thread
	}

	// Save thread mappings FIRST so the notifier can find them when the
	// GitHub webhook fires (race: webhook may arrive before agent returns).
	var firstCreatedIssueNumber int
	mappedIssues := map[int]bool{}
	for _, issue := range resp.SideEffects.CreatedIssues {
		if firstCreatedIssueNumber == 0 {
			firstCreatedIssueNumber = issue.Number
		}
		mappedIssues[issue.Number] = true
		thread, err := domain.NewSlackThread(
			target.RepoOwner, target.RepoName,
			issue.Number, 0,
			event.Channel, replyTs,
		)
		if err != nil {
			slog.Warn("failed to create thread mapping", "error", err)
			continue
		}
		if err := h.threadSaver.SaveThread(ctx, thread); err != nil {
			slog.Warn("failed to save thread mapping", "error", err, "issue", issue.Number)
		} else {
			slog.Info("saved thread mapping", "issue", issue.Number, "thread_ts", replyTs)
		}
	}

	for _, issueNum := range resp.SideEffects.DelegatedIssues {
		if mappedIssues[issueNum] {
			continue
		}
		mappedIssues[issueNum] = true
		thread, err := domain.NewSlackThread(
			target.RepoOwner, target.RepoName,
			issueNum, 0,
			event.Channel, replyTs,
		)
		if err != nil {
			slog.Warn("failed to create delegation thread mapping", "error", err)
			continue
		}
		if err := h.threadSaver.SaveThread(ctx, thread); err != nil {
			slog.Warn("failed to save delegation thread mapping", "error", err, "issue", issueNum)
		} else {
			slog.Info("saved delegation thread mapping", "issue", issueNum, "thread_ts", replyTs)
		}
	}

	// Synthesize fallback if agent returned no text but did create/delegate issues
	if resp.Text == "" && len(resp.SideEffects.CreatedIssues) > 0 {
		issue := resp.SideEffects.CreatedIssues[0]
		resp.Text = fmt.Sprintf("Created <https://github.com/%s/%s/issues/%d|#%d>: %s",
			target.RepoOwner, target.RepoName, issue.Number, issue.Number, issue.Title)
	}
	if resp.Text == "" && len(resp.SideEffects.DelegatedIssues) > 0 {
		var parts []string
		for _, num := range resp.SideEffects.DelegatedIssues {
			parts = append(parts, fmt.Sprintf("<https://github.com/%s/%s/issues/%d|#%d>",
				target.RepoOwner, target.RepoName, num, num))
		}
		resp.Text = fmt.Sprintf("Delegated to %s", strings.Join(parts, ", "))
	}

	if resp.Text != "" {
		if err := h.slackClient.PostToThread(ctx, target.SlackBotToken, event.Channel, replyTs,
			domain.MessageBlock{Text: resp.Text}); err != nil {
			slog.Error("failed to post thread reply", "error", err, "channel", event.Channel, "thread_ts", replyTs)
		}
	}

	// Record conversation for list_conversations support
	if h.conversationRecorder != nil {
		conv := agent.Conversation{
			ChannelID:    event.Channel,
			ThreadTs:     replyTs,
			Summary:      agent.TruncateSummary(text, 100),
			UserName:     displayName,
			LastActiveAt: time.Now(),
			LinkedIssue:  firstCreatedIssueNumber,
			RepoOwner:    target.RepoOwner,
			RepoName:     target.RepoName,
		}
		if err := h.conversationRecorder.UpsertConversation(ctx, conv); err != nil {
			slog.Warn("failed to record conversation", "error", err)
		}
	}
}

// resolveTargetByChannel resolves a Slack channel ID to a target config.
func (h *Handler) resolveTargetByChannel(ctx context.Context, channelID string) (targets.TargetConfig, bool) {
	channelName, err := h.resolveChannelName(ctx, channelID)
	if err != nil {
		slog.Debug("failed to resolve channel name", "channel", channelID, "error", err)
		return targets.TargetConfig{}, false
	}
	return h.registry.GetByChannelName(channelName)
}

// resolveChannelName uses the first available bot token to resolve a channel ID to its name.
func (h *Handler) resolveChannelName(ctx context.Context, channelID string) (string, error) {
	token := h.registry.AnyBotToken()
	if token == "" {
		return "", fmt.Errorf("no bot token available to resolve channel name")
	}
	return h.slackClient.GetChannelName(ctx, token, channelID)
}

// verifySignature validates the Slack request signature using HMAC-SHA256.
func (h *Handler) verifySignature(r *http.Request, body []byte) error {
	signature := r.Header.Get("X-Slack-Signature")
	timestamp := r.Header.Get("X-Slack-Request-Timestamp")

	if signature == "" || timestamp == "" {
		return fmt.Errorf("missing signature or timestamp headers")
	}

	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp: %w", err)
	}
	if math.Abs(float64(time.Now().Unix()-ts)) > 300 {
		return fmt.Errorf("timestamp too old")
	}

	sigBase := "v0:" + timestamp + ":" + string(body)
	mac := hmac.New(sha256.New, []byte(h.signingSecret))
	mac.Write([]byte(sigBase))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

func userFacingErrorMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "status 503") ||
		strings.Contains(msg, "status 502") ||
		strings.Contains(msg, "status 504"):
		return "The AI service is temporarily unavailable. Please try again in a few minutes."
	case strings.Contains(msg, "status 429") && strings.Contains(msg, "overloaded"):
		return "The AI service is currently overloaded. Please try again in a few minutes."
	case strings.Contains(msg, "status 429"):
		return "The AI service is rate-limited right now. Please try again shortly."
	default:
		return "Sorry, I encountered an error processing your request. Please try again."
	}
}
