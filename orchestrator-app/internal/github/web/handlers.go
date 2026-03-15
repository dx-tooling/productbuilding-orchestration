package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/github/domain"
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
	previewdomain "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
	slackfacade "github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/slack/facade"
)

// Notifier defines the interface for Slack notifications
type Notifier interface {
	Notify(ctx context.Context, event slackfacade.NotificationEvent, target targets.TargetConfig) error
}

// Registry defines the interface for target lookup
type Registry interface {
	Get(repoOwner, repoName string) (targets.TargetConfig, bool)
}

// PreviewService defines the interface for preview operations
type PreviewService interface {
	DeployPreview(ctx context.Context, req previewdomain.DeployRequest, pat string)
	DeletePreview(ctx context.Context, req previewdomain.DeployRequest, pat string)
}

type Handler struct {
	registry       Registry
	previewService PreviewService
	notifier       Notifier
}

func NewHandler(registry Registry, previewService PreviewService, notifier Notifier) *Handler {
	return &Handler{
		registry:       registry,
		previewService: previewService,
		notifier:       notifier,
	}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read webhook body", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")

	switch eventType {
	case "pull_request":
		h.handlePullRequest(w, r, body)
	case "issues":
		h.handleIssue(w, r, body)
	case "issue_comment":
		h.handleIssueComment(w, r, body)
	default:
		slog.Debug("ignoring webhook event", "event", eventType)
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) handlePullRequest(w http.ResponseWriter, r *http.Request, body []byte) {
	event, err := domain.ParsePREvent(body)
	if err != nil {
		slog.Error("failed to parse PR event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Look up target config for this repo
	target, ok := h.registry.Get(event.RepoOwner, event.RepoName)
	if !ok {
		slog.Warn("webhook from unknown repo", "repo", event.RepoOwner+"/"+event.RepoName)
		http.Error(w, "unknown repository", http.StatusNotFound)
		return
	}

	// Validate webhook signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if err := domain.ValidateSignature(body, sig, target.WebhookSecret); err != nil {
		slog.Warn("webhook signature validation failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("webhook received",
		"action", event.Action,
		"repo", event.RepoOwner+"/"+event.RepoName,
		"pr", event.PRNumber,
		"branch", event.Branch,
		"sha", event.HeadSHA,
	)

	// Send Slack notification for PR opened
	if event.Action == "opened" && h.notifier != nil {
		go h.notifySlackPR(slackfacade.EventPROpened, event, target)
	}

	req := previewdomain.DeployRequest{
		RepoOwner: event.RepoOwner,
		RepoName:  event.RepoName,
		PRNumber:  event.PRNumber,
		Branch:    event.Branch,
		HeadSHA:   event.HeadSHA,
	}

	switch event.Action {
	case "opened", "synchronize", "reopened":
		// Deploy/update preview asynchronously
		go h.previewService.DeployPreview(context.Background(), req, target.GitHubPAT)
	case "closed":
		// Tear down preview asynchronously
		go h.previewService.DeletePreview(context.Background(), req, target.GitHubPAT)
		// Notify Slack about PR close
		if h.notifier != nil {
			go h.notifySlackPR(slackfacade.EventPRClosed, event, target)
		}
	default:
		slog.Debug("ignoring PR action", "action", event.Action)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handleIssue(w http.ResponseWriter, r *http.Request, body []byte) {
	event, err := domain.ParseIssueEvent(body)
	if err != nil {
		slog.Error("failed to parse issue event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Look up target config for this repo
	target, ok := h.registry.Get(event.Repository.Owner.Login, event.Repository.Name)
	if !ok {
		slog.Warn("webhook from unknown repo", "repo", event.Repository.Owner.Login+"/"+event.Repository.Name)
		http.Error(w, "unknown repository", http.StatusNotFound)
		return
	}

	// Validate webhook signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if err := domain.ValidateSignature(body, sig, target.WebhookSecret); err != nil {
		slog.Warn("webhook signature validation failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("issue webhook received",
		"action", event.Action,
		"repo", event.Repository.Owner.Login+"/"+event.Repository.Name,
		"issue", event.Issue.Number,
		"title", event.Issue.Title,
	)

	// Send Slack notification
	if h.notifier != nil {
		var eventType slackfacade.EventType
		switch event.Action {
		case "opened":
			eventType = slackfacade.EventIssueOpened
		case "closed":
			eventType = slackfacade.EventIssueClosed
		case "reopened":
			eventType = slackfacade.EventIssueReopened
		default:
			w.WriteHeader(http.StatusOK)
			return
		}

		go h.notifySlackIssue(eventType, event, target)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) handleIssueComment(w http.ResponseWriter, r *http.Request, body []byte) {
	event, err := domain.ParseIssueCommentEvent(body)
	if err != nil {
		slog.Error("failed to parse issue comment event", "error", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Only handle created comments
	if event.Action != "created" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Skip comments posted from Slack or the agent to prevent echo loops
	if strings.Contains(event.Comment.Body, "<!-- via-slack -->") ||
		strings.Contains(event.Comment.Body, "<!-- via-agent -->") {
		slog.Debug("skipping comment originated from slack/agent", "comment_id", event.Comment.ID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// Look up target config for this repo
	target, ok := h.registry.Get(event.Repository.Owner.Login, event.Repository.Name)
	if !ok {
		slog.Warn("webhook from unknown repo", "repo", event.Repository.Owner.Login+"/"+event.Repository.Name)
		http.Error(w, "unknown repository", http.StatusNotFound)
		return
	}

	// Validate webhook signature
	sig := r.Header.Get("X-Hub-Signature-256")
	if err := domain.ValidateSignature(body, sig, target.WebhookSecret); err != nil {
		slog.Warn("webhook signature validation failed", "error", err)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slog.Info("issue comment webhook received",
		"repo", event.Repository.Owner.Login+"/"+event.Repository.Name,
		"issue", event.Issue.Number,
		"commenter", event.Comment.User.Login,
	)

	// Send Slack notification
	if h.notifier != nil {
		go h.notifySlackComment(event, target)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) notifySlackPR(eventType slackfacade.EventType, event *domain.PREvent, target targets.TargetConfig) {
	if h.notifier == nil || target.SlackChannel == "" {
		return
	}

	ctx := context.Background()
	slackEvent := slackfacade.NotificationEvent{
		Type:              eventType,
		RepoOwner:         event.RepoOwner,
		RepoName:          event.RepoName,
		IssueNumber:       event.PRNumber,
		Title:             event.Title,
		Body:              event.Body,
		Author:            event.Author,
		LinkedIssueNumber: domain.ExtractLinkedIssue(event.Body),
		URL:               fmt.Sprintf("https://github.com/%s/%s/pull/%d", event.RepoOwner, event.RepoName, event.PRNumber),
	}

	if err := h.notifier.Notify(ctx, slackEvent, target); err != nil {
		slog.Warn("failed to send slack notification", "error", err)
	}
}

func (h *Handler) notifySlackIssue(eventType slackfacade.EventType, event *domain.IssueEvent, target targets.TargetConfig) {
	if h.notifier == nil || target.SlackChannel == "" {
		return
	}

	ctx := context.Background()
	slackEvent := slackfacade.NotificationEvent{
		Type:        eventType,
		RepoOwner:   event.Repository.Owner.Login,
		RepoName:    event.Repository.Name,
		IssueNumber: event.Issue.Number,
		Title:       event.Issue.Title,
		Body:        event.Issue.Body,
		Author:      event.Issue.User.Login,
		URL:         fmt.Sprintf("https://github.com/%s/%s/issues/%d", event.Repository.Owner.Login, event.Repository.Name, event.Issue.Number),
	}

	if err := h.notifier.Notify(ctx, slackEvent, target); err != nil {
		slog.Warn("failed to send slack notification", "error", err)
	}
}

func (h *Handler) notifySlackComment(event *domain.IssueCommentEvent, target targets.TargetConfig) {
	if h.notifier == nil || target.SlackChannel == "" {
		return
	}

	ctx := context.Background()
	slackEvent := slackfacade.NotificationEvent{
		Type:        slackfacade.EventCommentAdded,
		RepoOwner:   event.Repository.Owner.Login,
		RepoName:    event.Repository.Name,
		IssueNumber: event.Issue.Number,
		Title:       event.Issue.Title,
		Body:        event.Comment.Body,
		Author:      event.Comment.User.Login,
		CommentID:   event.Comment.ID,
		URL:         fmt.Sprintf("https://github.com/%s/%s/issues/%d", event.Repository.Owner.Login, event.Repository.Name, event.Issue.Number),
	}

	if err := h.notifier.Notify(ctx, slackEvent, target); err != nil {
		slog.Warn("failed to send slack notification", "error", err)
	}
}
