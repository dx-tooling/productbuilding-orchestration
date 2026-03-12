package domain

import (
	"context"
	"time"
)

// SourceDownloader downloads and extracts repository source code.
type SourceDownloader interface {
	DownloadSource(ctx context.Context, owner, repo, sha, pat, destDir string) (extractedPath string, err error)
}

// ComposeManager manages Docker Compose projects for preview deployments.
type ComposeManager interface {
	GenerateOverride(workDir, serviceName, routerName, host string, port int) (overridePath string, err error)
	Up(ctx context.Context, projectName, workDir string, composeFiles []string) error
	Down(ctx context.Context, projectName, workDir string) error
	// Exec runs a command in a running container
	Exec(ctx context.Context, projectName, serviceName, workDir string, command []string) error
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
}

// DeployRequest is the input for deploying or updating a preview.
type DeployRequest struct {
	RepoOwner string
	RepoName  string
	PRNumber  int
	Branch    string
	HeadSHA   string
}
