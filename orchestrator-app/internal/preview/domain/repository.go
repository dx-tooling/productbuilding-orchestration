package domain

import "context"

type Repository interface {
	Upsert(ctx context.Context, p Preview) error
	FindByRepoPR(ctx context.Context, repoOwner, repoName string, prNumber int) (*Preview, error)
	ListActive(ctx context.Context) ([]Preview, error)
	UpdateStatus(ctx context.Context, id string, status Status) error
}
