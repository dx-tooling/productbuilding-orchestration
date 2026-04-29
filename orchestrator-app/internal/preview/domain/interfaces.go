package domain

import (
	"context"
	"io"
	"time"
)

// SourceDownloader downloads and extracts repository source code.
type SourceDownloader interface {
	DownloadSource(ctx context.Context, owner, repo, sha, pat, destDir string) (extractedPath string, err error)
}

// ComposeManager manages Docker Compose projects for preview deployments.
type ComposeManager interface {
	GenerateOverride(workDir, serviceName, routerName, host string, port int) (overridePath string, err error)
	Up(ctx context.Context, projectName, workDir string, composeFiles []string, extraEnv []string) error
	Down(ctx context.Context, projectName, workDir string) error
	// IsRunning reports whether at least one container belonging to the project is currently running.
	IsRunning(ctx context.Context, projectName string) (bool, error)
	// Exec runs a command in a running container
	Exec(ctx context.Context, projectName, serviceName, workDir string, command []string) error
	// Logs streams container logs to the provided writer
	Logs(ctx context.Context, projectName, serviceName string, tail int, follow bool, w io.Writer) error
	// LogsFromFile streams logs from files inside a container using tail
	LogsFromFile(ctx context.Context, projectName, serviceName, workDir, logPath string, tail int, follow bool, w io.Writer) error
}

// HealthChecker polls endpoints until they respond successfully.
type HealthChecker interface {
	WaitForHealthy(ctx context.Context, url string, timeout time.Duration) error
	WaitForTLS(ctx context.Context, url string, timeout time.Duration) error
}

// PRCommenter manages PR comments for status updates.
type PRCommenter interface {
	CreateComment(ctx context.Context, owner, repo string, prNumber int, body, pat string) (commentID int64, err error)
	UpdateComment(ctx context.Context, owner, repo string, commentID int64, body, pat string) error
	DeleteComment(ctx context.Context, owner, repo string, commentID int64, pat string) error
	DeleteAllBotComments(ctx context.Context, owner, repo string, prNumber int, pat string) error
}

// SlackThreadChecker checks whether a Slack thread is tracking a given PR.
type SlackThreadChecker interface {
	HasThread(ctx context.Context, repoOwner, repoName string, prNumber int) bool
}

// PullRequestStateChecker reports whether a given pull request is currently
// open on GitHub. Used by ReconcileActive to skip redeploying previews whose
// PRs have been closed while the orchestrator was offline.
type PullRequestStateChecker interface {
	IsPullRequestOpen(ctx context.Context, owner, repo string, prNumber int, pat string) (bool, error)
}

// DeployRequest is the input for deploying or updating a preview.
type DeployRequest struct {
	RepoOwner         string
	RepoName          string
	PRNumber          int
	Branch            string
	HeadSHA           string
	LinkedIssueNumber int // Issue number linked from PR body (e.g. "Fixes #16")
}
