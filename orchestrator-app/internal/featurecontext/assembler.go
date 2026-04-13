package featurecontext

import (
	"context"
	"log/slog"
)

// FeatureSnapshot aggregates all available context about a feature (issue + PR + CI + preview).
type FeatureSnapshot struct {
	Issue     *IssueState
	PR        *PRState
	CIStatus  CIStatus
	CIDetails []CheckRunState
	Preview   *PreviewState
}

type IssueState struct {
	Number int
	Title  string
	Body   string
	State  string
}

type PRState struct {
	Number    int
	Title     string
	State     string
	Merged    bool
	HeadSHA   string
	HeadRef   string
	Author    string
	Additions int
	Deletions int
	URL       string
}

type CIStatus string

const (
	CIUnknown CIStatus = "unknown"
	CIPending CIStatus = "pending"
	CIPassing CIStatus = "passing"
	CIFailing CIStatus = "failing"
)

type CheckRunState struct {
	Name       string
	Conclusion string
	URL        string
}

type PreviewState struct {
	Status string
	URL    string
}

// IssueGetter fetches issue details.
type IssueGetter interface {
	GetIssue(ctx context.Context, owner, repo string, number int, pat string) (*IssueState, error)
}

// PRGetter fetches pull request details.
type PRGetter interface {
	GetPR(ctx context.Context, owner, repo string, number int, pat string) (*PRState, error)
}

// CheckRunGetter fetches check run results for a ref.
type CheckRunGetter interface {
	GetCheckRunsForRef(ctx context.Context, owner, repo, ref, pat string) ([]CheckRunState, error)
}

// PreviewGetter fetches preview deployment state.
type PreviewGetter interface {
	GetPreview(ctx context.Context, owner, repo string, prNumber int) (*PreviewState, error)
}

// Assembler gathers feature context from multiple sources.
type Assembler struct {
	issues   IssueGetter
	prs      PRGetter
	checks   CheckRunGetter
	previews PreviewGetter
}

func NewAssembler(issues IssueGetter, prs PRGetter, checks CheckRunGetter, previews PreviewGetter) *Assembler {
	return &Assembler{
		issues:   issues,
		prs:      prs,
		checks:   checks,
		previews: previews,
	}
}

// ForPR assembles a full feature snapshot for a pull request.
func (a *Assembler) ForPR(ctx context.Context, owner, repo, pat string, prNumber, linkedIssue int) (*FeatureSnapshot, error) {
	pr, err := a.prs.GetPR(ctx, owner, repo, prNumber, pat)
	if err != nil {
		return nil, err
	}

	snap := &FeatureSnapshot{
		PR:       pr,
		CIStatus: CIUnknown,
	}

	// Linked issue is a soft failure — log and continue
	if linkedIssue > 0 {
		issue, err := a.issues.GetIssue(ctx, owner, repo, linkedIssue, pat)
		if err != nil {
			slog.Warn("failed to fetch linked issue", "issue", linkedIssue, "error", err)
		} else {
			snap.Issue = issue
		}
	}

	// Fetch check runs using the PR's head SHA
	if pr.HeadSHA != "" {
		checks, err := a.checks.GetCheckRunsForRef(ctx, owner, repo, pr.HeadSHA, pat)
		if err != nil {
			slog.Warn("failed to fetch check runs", "ref", pr.HeadSHA, "error", err)
		} else {
			snap.CIDetails = checks
			snap.CIStatus = deriveCIStatus(checks)
		}
	}

	// Fetch preview state
	preview, err := a.previews.GetPreview(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("failed to fetch preview", "pr", prNumber, "error", err)
	} else {
		snap.Preview = preview
	}

	return snap, nil
}

// ForIssue assembles a feature snapshot for an issue (no PR/CI/preview).
func (a *Assembler) ForIssue(ctx context.Context, owner, repo, pat string, number int) (*FeatureSnapshot, error) {
	issue, err := a.issues.GetIssue(ctx, owner, repo, number, pat)
	if err != nil {
		return nil, err
	}

	return &FeatureSnapshot{
		Issue:    issue,
		CIStatus: CIUnknown,
	}, nil
}

// deriveCIStatus determines the aggregate CI status from individual check runs.
// Priority: any failure -> CIFailing, any pending/in_progress -> CIPending, all success -> CIPassing.
func deriveCIStatus(checks []CheckRunState) CIStatus {
	if len(checks) == 0 {
		return CIUnknown
	}

	hasFailure := false
	hasPending := false
	for _, c := range checks {
		switch c.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required":
			hasFailure = true
		case "":
			// Empty conclusion means still running
			hasPending = true
		case "success", "neutral", "skipped":
			// OK
		default:
			hasPending = true
		}
	}

	if hasFailure {
		return CIFailing
	}
	if hasPending {
		return CIPending
	}
	return CIPassing
}
