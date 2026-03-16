package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/platform/targets"
)

// Router classifies user intent via a single LLM call and returns a RoutingDecision.
type Router struct {
	llm LLMClient
}

// NewRouter creates a new Router.
func NewRouter(llm LLMClient) *Router {
	return &Router{llm: llm}
}

// researcherFallback is the default when routing fails.
var researcherFallback = RoutingDecision{
	Steps: []RoutingStep{{Specialist: "researcher", Params: map[string]string{}, Reasoning: "fallback"}},
}

// Route makes one LLM call and returns a RoutingDecision.
func (r *Router) Route(ctx context.Context, userText string, target targets.TargetConfig, linkedIssue *IssueContext, threadMessages []ThreadMessage) (RoutingDecision, error) {
	systemPrompt := renderRouterPrompt(target.RepoOwner, target.RepoName)

	userMsg := userText
	if linkedIssue != nil {
		userMsg += fmt.Sprintf("\n\n[This Slack thread is linked to GitHub issue #%d: %q (state: %s)]",
			linkedIssue.Number, linkedIssue.Title, linkedIssue.State)
	}
	if summary := formatThreadSummaryForRouter(threadMessages); summary != "" {
		userMsg += "\n\n" + summary
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	}

	resp, err := r.llm.ChatCompletion(ctx, ChatRequest{
		Messages: messages,
	})
	if err != nil {
		return RoutingDecision{}, fmt.Errorf("router llm call: %w", err)
	}

	decision, parseErr := parseRoutingJSON(resp.Content)
	if parseErr != nil {
		slog.Warn("router: failed to parse routing JSON, falling back to researcher",
			"error", parseErr,
			"response_preview", truncateForLog(resp.Content, 200),
		)
		return researcherFallback, nil
	}

	if len(decision.Steps) == 0 {
		slog.Warn("router: empty steps, falling back to researcher")
		return researcherFallback, nil
	}

	return decision, nil
}

// formatThreadSummaryForRouter builds a compact summary of the last few thread
// messages so the router can resolve ambiguous follow-ups. Returns "" if there
// are no messages to summarize.
func formatThreadSummaryForRouter(msgs []ThreadMessage) string {
	if len(msgs) == 0 {
		return ""
	}

	const maxMessages = 5
	const maxCharPerMsg = 200
	const maxTotal = 2000

	start := 0
	if len(msgs) > maxMessages {
		start = len(msgs) - maxMessages
	}
	recent := msgs[start:]

	var sb strings.Builder
	sb.WriteString("[Conversation history in this thread:\n")
	for _, m := range recent {
		role := "User"
		if m.BotID != "" {
			role = "Assistant"
		}
		text := m.Text
		if len(text) > maxCharPerMsg {
			text = text[:maxCharPerMsg] + "..."
		}
		line := fmt.Sprintf("- %s: %q\n", role, text)
		if sb.Len()+len(line) > maxTotal {
			break
		}
		sb.WriteString(line)
	}
	sb.WriteString("]")
	return sb.String()
}

// codeFencePattern strips ```json ... ``` wrappers.
var codeFencePattern = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")

// parseRoutingJSON extracts and parses a RoutingDecision from the LLM's text response.
// Tolerates leading text before JSON and trailing text after it.
func parseRoutingJSON(text string) (RoutingDecision, error) {
	// Try stripping code fences first
	if m := codeFencePattern.FindStringSubmatch(text); len(m) > 1 {
		text = m[1]
	}

	// Find the first { to handle any leading text
	idx := strings.Index(text, "{")
	if idx < 0 {
		return RoutingDecision{}, fmt.Errorf("no JSON object found in response")
	}
	text = text[idx:]

	var decision RoutingDecision
	dec := json.NewDecoder(strings.NewReader(text))
	if err := dec.Decode(&decision); err != nil {
		return RoutingDecision{}, fmt.Errorf("unmarshal routing decision: %w", err)
	}
	return decision, nil
}
