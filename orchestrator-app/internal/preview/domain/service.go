package domain

import (
	"context"
	"fmt"
)

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) ListPreviews(ctx context.Context) ([]Preview, error) {
	return s.repo.ListActive(ctx)
}

func (s *Service) GetPreview(ctx context.Context, repoOwner, repoName string, prNumber int) (*Preview, error) {
	p, err := s.repo.FindByRepoPR(ctx, repoOwner, repoName, prNumber)
	if err != nil {
		return nil, fmt.Errorf("find preview: %w", err)
	}
	return p, nil
}
