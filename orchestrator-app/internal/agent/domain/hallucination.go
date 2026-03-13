package domain

import "strings"

// hallucinationRule defines a claim pattern and how to verify it against side effects.
type hallucinationRule struct {
	patterns   []string
	check      func(SideEffects) bool
	correction string
}

var hallucinationRules = []hallucinationRule{
	{
		patterns: []string{
			"i've asked opencode",
			"i've delegated",
			"i triggered opencode",
			"asked opencode to",
			"delegated to opencode",
		},
		check: func(e SideEffects) bool { return len(e.DelegatedIssues) > 0 },
		correction: "You claimed to delegate to OpenCode, but no /opencode comment was actually posted. " +
			"Do not claim actions you haven't performed. Please use the add_github_comment tool to actually post the /opencode command now.",
	},
	{
		patterns: []string{
			"i've created issue",
			"i created issue",
			"created issue #",
			"opened issue #",
		},
		check: func(e SideEffects) bool { return len(e.CreatedIssues) > 0 },
		correction: "You claimed to create an issue, but no issue was actually created. " +
			"Do not claim actions you haven't performed. Please use the create_github_issue tool to actually create the issue now.",
	},
	{
		patterns: []string{
			"i've posted a comment",
			"i posted a comment",
			"i've added a comment",
			"i added a comment",
		},
		check: func(e SideEffects) bool { return len(e.PostedComments) > 0 },
		correction: "You claimed to post a comment, but no comment was actually posted. " +
			"Do not claim actions you haven't performed. Please use the add_github_comment tool to actually post the comment now.",
	},
}

// DetectHallucination checks if the LLM's response text claims an action that
// is not backed by actual side effects. Returns a correction message if a
// hallucination is detected, or "" if the response is clean.
func DetectHallucination(responseText string, effects SideEffects) string {
	lower := strings.ToLower(responseText)
	for _, rule := range hallucinationRules {
		for _, pattern := range rule.patterns {
			if strings.Contains(lower, pattern) && !rule.check(effects) {
				return rule.correction
			}
		}
	}
	return ""
}

// truncateForLog truncates a string for structured log fields.
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
