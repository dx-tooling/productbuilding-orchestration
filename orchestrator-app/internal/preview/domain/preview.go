package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusBuilding  Status = "building"
	StatusDeploying Status = "deploying"
	StatusReady     Status = "ready"
	StatusFailed    Status = "failed"
	StatusDeleted   Status = "deleted"
)

type Preview struct {
	ID                string
	RepoOwner         string
	RepoName          string
	PRNumber          int
	BranchName        string
	HeadSHA           string
	PreviewURL        string
	Status            Status
	ComposeProject    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	LastSuccessfulSHA string
	ErrorStage        string
	ErrorMessage      string
	GithubCommentID   int64
	LinkedIssueNumber int // transient — not persisted, carried from DeployRequest for notifications
}

func NewPreview(repoOwner, repoName string, prNumber int, branchName, headSHA, previewDomain string) Preview {
	composeProject := fmt.Sprintf("%s_pr_%d", repoName, prNumber)
	previewURL := fmt.Sprintf("https://%s-pr-%d.%s", repoName, prNumber, previewDomain)

	return Preview{
		ID:             uuid.New().String(),
		RepoOwner:      repoOwner,
		RepoName:       repoName,
		PRNumber:       prNumber,
		BranchName:     branchName,
		HeadSHA:        headSHA,
		PreviewURL:     previewURL,
		Status:         StatusPending,
		ComposeProject: composeProject,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}
