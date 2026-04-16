package domain

import (
	"strings"
	"testing"
)

func TestRenderRouterPrompt_ContainsLanguageInstruction(t *testing.T) {
	prompt := renderRouterPrompt("acme", "widgets", "de")
	if !strings.Contains(prompt, "MUST respond in de") {
		t.Error("expected router prompt to contain language instruction for 'de'")
	}
}

func TestRenderRouterPrompt_ContainsEventNarratorRouting(t *testing.T) {
	prompt := renderRouterPrompt("acme", "widgets", "en")

	if !strings.Contains(prompt, "event_narrator") {
		t.Error("expected router prompt to contain event_narrator specialist")
	}
	if !strings.Contains(prompt, "[system event]") {
		t.Error("expected router prompt to mention [system event] prefix routing")
	}
}

func TestRenderRouterPrompt_ContainsPhaseGuidance(t *testing.T) {
	prompt := renderRouterPrompt("acme", "widgets", "en")

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
