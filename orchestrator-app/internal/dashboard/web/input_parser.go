package web

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type QueryType int

const (
	QueryIssue  QueryType = iota // #123
	QueryGitHub                  // GitHub issue/PR URL
	QuerySlack                   // Slack message/thread URL
)

type InvestigationQuery struct {
	Type           QueryType
	Number         int    // issue/PR number (QueryIssue and QueryGitHub)
	Owner          string // GitHub owner (QueryGitHub only)
	Repo           string // GitHub repo (QueryGitHub only)
	SlackChannel   string // Slack channel ID (QuerySlack only)
	SlackTs        string // message timestamp (QuerySlack only)
	SlackThreadTs  string // parent thread timestamp (QuerySlack only)
}

var (
	issueNumberRe = regexp.MustCompile(`^#(\d+)$`)
	githubURLRe   = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(?:issues|pull)/(\d+)`)
	slackURLRe    = regexp.MustCompile(`^https://[^/]+\.slack\.com/archives/([^/]+)/p(\d+)`)
)

func ParseInvestigationInput(input string) (InvestigationQuery, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return InvestigationQuery{}, fmt.Errorf("empty input")
	}

	// #123
	if m := issueNumberRe.FindStringSubmatch(input); m != nil {
		num, _ := strconv.Atoi(m[1])
		return InvestigationQuery{Type: QueryIssue, Number: num}, nil
	}

	// GitHub URL
	if m := githubURLRe.FindStringSubmatch(input); m != nil {
		num, _ := strconv.Atoi(m[3])
		return InvestigationQuery{
			Type:   QueryGitHub,
			Owner:  m[1],
			Repo:   m[2],
			Number: num,
		}, nil
	}

	// Slack URL
	if m := slackURLRe.FindStringSubmatch(input); m != nil {
		channel := m[1]
		// Convert p-timestamp: p1773605494857279 → "1773605494.857279"
		rawTs := m[2]
		slackTs := convertPTimestamp(rawTs)

		q := InvestigationQuery{
			Type:         QuerySlack,
			SlackChannel: channel,
			SlackTs:      slackTs,
		}

		// Check for thread_ts query parameter
		if u, err := url.Parse(input); err == nil {
			if threadTs := u.Query().Get("thread_ts"); threadTs != "" {
				q.SlackThreadTs = threadTs
			}
		}

		return q, nil
	}

	return InvestigationQuery{}, fmt.Errorf("unrecognized input format: %q", input)
}

// convertPTimestamp converts a Slack p-timestamp (e.g., "1773605494857279")
// to a Slack ts format (e.g., "1773605494.857279").
func convertPTimestamp(pts string) string {
	if len(pts) <= 10 {
		return pts
	}
	return pts[:10] + "." + pts[10:]
}
