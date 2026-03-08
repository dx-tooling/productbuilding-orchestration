package facade

import "time"

type PreviewDTO struct {
	ID             string
	RepoOwner      string
	RepoName       string
	PRNumber       int
	BranchName     string
	HeadSHA        string
	PreviewURL     string
	Status         string
	ComposeProject string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ErrorMessage   string
}
