package testharness

import (
	"github.com/luminor-project/luminor-productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func NewPreview(opts ...func(*domain.Preview)) domain.Preview {
	p := domain.NewPreview("luminor-project", "etfg-app-starter-kit", 42, "feature/test", "abc123def", "productbuilder.luminor-tech.net")
	for _, opt := range opts {
		opt(&p)
	}
	return p
}

func WithStatus(s domain.Status) func(*domain.Preview) {
	return func(p *domain.Preview) { p.Status = s }
}

func WithPR(number int) func(*domain.Preview) {
	return func(p *domain.Preview) { p.PRNumber = number }
}
