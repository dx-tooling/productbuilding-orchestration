package domain

import "regexp"

var mrkdwnHeadingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)`)

// MarkdownToMrkdwn converts common Markdown formatting patterns to Slack's
// mrkdwn syntax. Designed to post-process LLM output before sending to Slack.
func MarkdownToMrkdwn(s string) string {
	// ### heading → *heading* (must run before bold to avoid double-wrapping)
	s = mrkdwnHeadingRe.ReplaceAllString(s, "*$1*")
	// **bold** → *bold* (reuses boldRe from notifier.go)
	// Run twice: heading conversion can produce ***text*** which needs two passes
	// (***x*** → **x** → *x*).
	s = boldRe.ReplaceAllString(s, "*$1*")
	s = boldRe.ReplaceAllString(s, "*$1*")
	return s
}
