package domain

import (
	"strings"
	"testing"
)

func TestRenderRouterPrompt_ContainsPhaseGuidance(t *testing.T) {
	prompt := renderRouterPrompt("acme", "widgets")

	// The router prompt should include guidance about workstream phases
	if !strings.Contains(prompt, "Workstream phase") {
		t.Error("expected router prompt to contain workstream phase guidance")
	}

	// It should mention key phases and their routing implications
	phases := []string{"review", "revision", "intake"}
	for _, phase := range phases {
		if !strings.Contains(prompt, phase) {
			t.Errorf("expected router prompt to mention phase %q", phase)
		}
	}
}
