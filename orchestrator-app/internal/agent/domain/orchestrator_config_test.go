package domain

import (
	"testing"
)

func TestSpecialistConfigs_ResearcherHasEnoughIterations(t *testing.T) {
	specs := defaultSpecialistConfigs()
	researcher, ok := specs["researcher"]
	if !ok {
		t.Fatal("researcher specialist config not found")
	}
	if researcher.MaxIterations < 10 {
		t.Errorf("researcher MaxIterations=%d, want >= 10 (code exploration needs multiple search+read cycles)",
			researcher.MaxIterations)
	}
}

func TestSpecialistConfigs_MaxIterationsAllowRecovery(t *testing.T) {
	// All specialists should have at least 5 iterations to allow for:
	// - Normal execution (2-3 iterations)
	// - Hallucination detection + correction (consumes 1 extra iteration)
	// - Recovery after correction (1 more iteration)
	const minIterations = 5

	specs := defaultSpecialistConfigs()

	for name, cfg := range specs {
		// Tool-free specialists (e.g. event_narrator) don't need recovery iterations
		if len(cfg.ToolDefs) == 0 {
			continue
		}
		if cfg.MaxIterations < minIterations {
			t.Errorf("specialist %q has MaxIterations=%d, want >= %d (must allow hallucination recovery)",
				name, cfg.MaxIterations, minIterations)
		}
	}
}
