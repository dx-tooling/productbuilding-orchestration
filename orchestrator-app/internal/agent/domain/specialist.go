package domain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// reroutePattern matches [REROUTE:specialist_name] signals in specialist text output.
var reroutePattern = regexp.MustCompile(`\[REROUTE:(\w+)\]`)

// extractReroute returns the target specialist name if a reroute signal is present, or "".
func extractReroute(text string) string {
	m := reroutePattern.FindStringSubmatch(text)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// stripReroute removes the [REROUTE:...] signal from text.
func stripReroute(text string) string {
	return strings.TrimSpace(reroutePattern.ReplaceAllString(text, ""))
}

// SpecialistConfig defines a specialist's prompt, tools, and iteration limit.
type SpecialistConfig struct {
	Name           string
	PromptTemplate *template.Template // Preferred: pre-parsed template
	SystemPrompt   string             // Fallback: raw Go template string (used if PromptTemplate is nil)
	ToolDefs       []ToolDef
	MaxIterations  int
}

// Specialist is a focused agent that handles one type of task.
type Specialist struct {
	config             SpecialistConfig
	llm                LLMClient
	tools              ToolExecutor
	slackFetcher       SlackThreadFetcher
	conversationLister ConversationLister
	workspace          string
	tokenBudget        TokenBudget
}

// Run executes the specialist's focused agent loop.
func (s *Specialist) Run(ctx context.Context, req RunRequest, prior *PriorStepContext) (SpecialistResult, error) {
	s.tools.ResetEffects()
	if req.OnIssueCreated != nil {
		s.tools.SetOnIssueCreated(req.OnIssueCreated)
	}

	systemPrompt, err := s.renderPrompt(req)
	if err != nil {
		return SpecialistResult{}, fmt.Errorf("render specialist prompt: %w", err)
	}

	// Use pre-fetched thread messages, or fetch if not provided
	var threadMsgs []ThreadMessage
	if req.ThreadMessages != nil {
		for _, msg := range req.ThreadMessages {
			if msg.Ts == req.MessageTs {
				continue
			}
			threadMsgs = append(threadMsgs, msg)
		}
	} else if req.ThreadTs != "" && s.slackFetcher != nil {
		fetched, fetchErr := s.slackFetcher.GetThreadReplies(ctx, req.Target.SlackBotToken, req.ChannelID, req.ThreadTs)
		if fetchErr != nil {
			slog.Warn("specialist: failed to fetch thread replies", "specialist", s.config.Name, "error", fetchErr)
		} else {
			for _, msg := range fetched {
				if msg.Ts == req.MessageTs {
					continue
				}
				threadMsgs = append(threadMsgs, msg)
			}
		}
	}

	userMessage := fmt.Sprintf("%s says: %s", req.UserName, req.UserText)
	messages := BuildContext(systemPrompt, userMessage, threadMsgs, req.LinkedIssue, req.FeatureSummary, s.tokenBudget)

	// Inject prior step context if chaining
	if prior != nil {
		priorMsg := fmt.Sprintf("Previous step (%s) result: %s", prior.StepName, prior.ResultText)
		if len(prior.Effects.CreatedIssues) > 0 {
			priorMsg += fmt.Sprintf("\nCreated issue #%d: %s",
				prior.Effects.CreatedIssues[0].Number, prior.Effects.CreatedIssues[0].Title)
		}
		// Insert prior context as a system message before the user message
		messages = append(messages[:len(messages)-1],
			Message{Role: "system", Content: priorMsg},
			messages[len(messages)-1],
		)
	}

	// Initialize trace step for this specialist
	var traceStep *StepTrace
	if t := TraceFromContext(ctx); t != nil {
		t.Steps = append(t.Steps, StepTrace{Specialist: s.config.Name})
		traceStep = &t.Steps[len(t.Steps)-1]
	}

	// Agent loop
	for i := 0; i < s.config.MaxIterations; i++ {
		slog.Info("specialist llm call",
			"specialist", s.config.Name,
			"iteration", i+1,
			"channel", req.ChannelID,
			"message_count", len(messages),
		)

		llmStart := time.Now()
		resp, err := s.llm.ChatCompletion(ctx, ChatRequest{
			Messages: messages,
			Tools:    s.config.ToolDefs,
		})
		llmLatency := time.Since(llmStart).Milliseconds()
		if err != nil {
			return SpecialistResult{}, fmt.Errorf("specialist %s llm completion: %w", s.config.Name, err)
		}

		// Start recording this iteration
		var traceIter *IterationTrace
		if traceStep != nil {
			traceStep.Iterations = append(traceStep.Iterations, IterationTrace{
				MessageCount:  len(messages),
				InputMessages: messagesToTrace(messages),
				LLMContent:    resp.Content,
				FinishReason:  resp.FinishReason,
				LatencyMs:     llmLatency,
			})
			traceIter = &traceStep.Iterations[len(traceStep.Iterations)-1]
		}

		// Text response (no tool calls) — check for hallucination (action specialists only)
		if resp.FinishReason == "stop" || len(resp.ToolCalls) == 0 {
			if correction := DetectHallucinationForSpecialist(s.config.Name, resp.Content, s.tools.Effects()); correction != "" {
				slog.Warn("specialist hallucination detected",
					"specialist", s.config.Name,
					"iteration", i+1,
				)
				messages = append(messages, Message{Role: "assistant", Content: resp.Content})
				messages = append(messages, Message{Role: "user", Content: correction})
				continue
			}

			result := SpecialistResult{
				Text:        resp.Content,
				SideEffects: s.tools.Effects(),
			}

			// Check for reroute signal
			if target := extractReroute(resp.Content); target != "" {
				result.Text = stripReroute(resp.Content)
				result.Reroute = target
				slog.Info("specialist signaled reroute",
					"specialist", s.config.Name,
					"target", target,
				)
			}

			return result, nil
		}

		// Tool calls
		for _, tc := range resp.ToolCalls {
			slog.Info("specialist tool call",
				"specialist", s.config.Name,
				"tool", tc.Function.Name,
				"tool_call_id", tc.ID,
			)
		}

		messages = append(messages, Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		for _, tc := range resp.ToolCalls {
			var result string
			var execErr error

			toolStart := time.Now()
			if tc.Function.Name == "list_conversations" && s.conversationLister != nil {
				result, execErr = s.executeListConversations(ctx, tc, req.ChannelID)
			} else {
				result, execErr = s.tools.Execute(ctx, tc, req.Target)
			}
			toolLatency := time.Since(toolStart).Milliseconds()

			if execErr != nil {
				slog.Warn("specialist tool execution failed",
					"specialist", s.config.Name,
					"tool", tc.Function.Name,
					"error", execErr,
				)
				result = fmt.Sprintf("Error: %s", execErr.Error())
			}

			// Record tool call in trace
			if traceIter != nil {
				tcTrace := ToolCallTrace{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
					LatencyMs: toolLatency,
				}
				if execErr != nil {
					tcTrace.Error = execErr.Error()
				} else {
					tcTrace.Result = result
				}
				traceIter.ToolCalls = append(traceIter.ToolCalls, tcTrace)
			}

			messages = append(messages, Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			})
		}
	}

	// Max iterations reached — salvage partial findings from the conversation
	slog.Warn("specialist max iterations reached",
		"specialist", s.config.Name,
		"channel", req.ChannelID,
	)

	// Walk backwards: prefer the last assistant text, fall back to last tool result
	var partial string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" && strings.TrimSpace(messages[i].Content) != "" {
			partial = messages[i].Content
			break
		}
	}
	if partial == "" {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "tool" && strings.TrimSpace(messages[i].Content) != "" {
				partial = messages[i].Content
				break
			}
		}
	}

	var text string
	if partial != "" {
		text = "I wasn't able to fully answer, but here's what I found:\n\n" + partial
	} else {
		text = "I ran out of steps before I could put together an answer. Could you try a more specific question?"
	}

	return SpecialistResult{
		Text:        text,
		SideEffects: s.tools.Effects(),
	}, nil
}

// renderPrompt renders the specialist's system prompt template with repo context.
func (s *Specialist) renderPrompt(req RunRequest) (string, error) {
	data := PromptData{
		RepoOwner: req.Target.RepoOwner,
		RepoName:  req.Target.RepoName,
	}

	var buf bytes.Buffer
	if s.config.PromptTemplate != nil {
		if err := s.config.PromptTemplate.Execute(&buf, data); err != nil {
			return "", fmt.Errorf("execute specialist prompt template: %w", err)
		}
		return buf.String(), nil
	}

	tmpl, err := template.New("specialist").Parse(s.config.SystemPrompt)
	if err != nil {
		return "", fmt.Errorf("parse specialist prompt template: %w", err)
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute specialist prompt template: %w", err)
	}
	return buf.String(), nil
}

// executeListConversations handles the list_conversations tool call within a specialist.
func (s *Specialist) executeListConversations(ctx context.Context, tc ToolCall, channelID string) (string, error) {
	var args struct {
		Days int `json:"days"`
	}
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		return "", fmt.Errorf("parse arguments: %w", err)
	}
	if args.Days <= 0 {
		args.Days = 14
	}

	convs, err := s.conversationLister.ListRecentConversations(ctx, channelID, args.Days)
	if err != nil {
		return "", fmt.Errorf("list conversations: %w", err)
	}

	if len(convs) == 0 {
		return fmt.Sprintf("No conversations found in this channel in the last %d days.", args.Days), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d conversation(s) in the last %d days:\n\n", len(convs), args.Days))
	for _, c := range convs {
		link := SlackThreadLink(s.workspace, c.ChannelID, c.ThreadTs)
		summary := c.Summary
		if summary == "" {
			summary = "(no summary)"
		}
		sb.WriteString(fmt.Sprintf("- %s (by %s) — <%s|view thread>\n", summary, c.UserName, link))
	}
	return sb.String(), nil
}

// messagesToTrace converts LLM messages to trace format, truncating long content.
func messagesToTrace(msgs []Message) []TraceLLMMessage {
	const maxContentLen = 2000
	result := make([]TraceLLMMessage, len(msgs))
	for i, m := range msgs {
		content := m.Content
		if m.Role == "system" && len(content) > maxContentLen {
			content = content[:maxContentLen] + "... (truncated)"
		}
		// For tool result messages, include the tool call ID as context
		if m.Role == "tool" && m.ToolCallID != "" {
			content = "[tool_call_id: " + m.ToolCallID + "]\n" + content
		}
		result[i] = TraceLLMMessage{Role: m.Role, Content: content}
	}
	return result
}
