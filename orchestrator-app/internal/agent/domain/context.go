package domain

import "fmt"

// TokenBudget controls how much context the agent uses.
type TokenBudget struct {
	Total             int // total token budget for all context
	IssueMaxTokens    int // max tokens for linked issue body
	ThreadMaxMessages int // max thread messages to include
}

// DefaultTokenBudget returns sensible defaults.
func DefaultTokenBudget() TokenBudget {
	return TokenBudget{
		Total:             8000,
		IssueMaxTokens:    1000,
		ThreadMaxMessages: 20,
	}
}

// EstimateTokens provides a rough token count using ~4 chars/token heuristic.
func EstimateTokens(text string) int {
	return len(text) / 4
}

// BuildContext assembles the LLM message list with token-aware truncation.
// Priority: system prompt (always) → user message (always) → thread history (most recent first) → linked issue (truncated).
func BuildContext(
	systemPrompt string,
	userMessage string,
	threadMessages []ThreadMessage,
	linkedIssue *IssueContext,
	budget TokenBudget,
) []Message {
	var messages []Message

	// 1. System prompt (always included)
	sysMsg := Message{Role: "system", Content: systemPrompt}
	messages = append(messages, sysMsg)
	usedTokens := EstimateTokens(systemPrompt)

	// 2. Reserve tokens for user message (always included, added last)
	userTokens := EstimateTokens(userMessage)
	usedTokens += userTokens

	// 3. Linked issue context (truncated to IssueMaxTokens)
	var issueMsg *Message
	if linkedIssue != nil {
		issueBody := linkedIssue.Body
		maxBodyChars := budget.IssueMaxTokens * 4 // reverse the 4 chars/token heuristic

		prefix := fmt.Sprintf("[Context: This thread is linked to GitHub issue #%d (%s) — state: %s]\n\n",
			linkedIssue.Number, linkedIssue.Title, linkedIssue.State)
		prefixTokens := EstimateTokens(prefix)
		bodyBudgetChars := maxBodyChars - (prefixTokens * 4)
		if bodyBudgetChars < 0 {
			bodyBudgetChars = 0
		}

		if len(issueBody) > bodyBudgetChars {
			issueBody = issueBody[:bodyBudgetChars] + "\n\n[... truncated]"
		}

		content := prefix + issueBody
		issueTokens := EstimateTokens(content)

		if usedTokens+issueTokens <= budget.Total {
			msg := Message{Role: "system", Content: content}
			issueMsg = &msg
			usedTokens += issueTokens
		}
	}

	// 4. Thread history (most recent messages first, drop oldest if over budget)
	var threadMsgs []Message
	if len(threadMessages) > 0 {
		// Cap at ThreadMaxMessages (keep most recent)
		start := 0
		if len(threadMessages) > budget.ThreadMaxMessages {
			start = len(threadMessages) - budget.ThreadMaxMessages
		}
		trimmed := threadMessages[start:]

		remainingTokens := budget.Total - usedTokens
		// Build thread messages, but respect token budget
		for _, msg := range trimmed {
			role := "user"
			if msg.BotID != "" {
				role = "assistant"
			}
			tokens := EstimateTokens(msg.Text)
			if remainingTokens-tokens < 0 {
				break
			}
			remainingTokens -= tokens
			threadMsgs = append(threadMsgs, Message{Role: role, Content: msg.Text})
		}
	}

	// Assemble: system → thread → issue → user
	messages = append(messages, threadMsgs...)

	if issueMsg != nil {
		messages = append(messages, *issueMsg)
	}

	messages = append(messages, Message{Role: "user", Content: userMessage})

	return messages
}
