package testharness

import (
	"github.com/dx-tooling/productbuilding-orchestration/orchestrator-app/internal/preview/domain"
)

func NewPreview(opts ...func(*domain.Preview)) domain.Preview {
	p := domain.NewPreview("example-org", "my-app", 42, "feature/test", "abc123def", "preview.example.com")
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
