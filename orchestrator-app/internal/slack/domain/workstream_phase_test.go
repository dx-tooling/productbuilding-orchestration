package domain

import "testing"

func TestWorkstreamPhase_ValidPhases(t *testing.T) {
	validPhases := []WorkstreamPhase{
		PhaseIntake,
		PhaseOpen,
		PhaseInProgress,
		PhaseReview,
		PhaseRevision,
		PhaseDone,
		PhaseAbandoned,
	}

	for _, phase := range validPhases {
		if !phase.IsValid() {
			t.Errorf("expected phase %q to be valid", phase)
		}
	}
}

func TestWorkstreamPhase_InvalidPhase(t *testing.T) {
	invalid := WorkstreamPhase("nonsense")
	if invalid.IsValid() {
		t.Errorf("expected phase %q to be invalid", invalid)
	}
}

func TestWorkstreamPhase_EmptyIsInvalid(t *testing.T) {
	empty := WorkstreamPhase("")
	if empty.IsValid() {
		t.Errorf("expected empty phase to be invalid")
	}
}

func TestSlackThread_HasWorkstreamPhaseField(t *testing.T) {
	thread := SlackThread{
		WorkstreamPhase: PhaseOpen,
	}
	if thread.WorkstreamPhase != PhaseOpen {
		t.Errorf("expected phase %q, got %q", PhaseOpen, thread.WorkstreamPhase)
	}
}

func TestSlackThread_HasPreviewNotifiedAt(t *testing.T) {
	thread := SlackThread{}
	// PreviewNotifiedAt should be nil by default (pointer to time.Time)
	if thread.PreviewNotifiedAt != nil {
		t.Error("expected PreviewNotifiedAt to be nil by default")
	}
}

func TestSlackThread_HasFeedbackRelayed(t *testing.T) {
	thread := SlackThread{}
	if thread.FeedbackRelayed {
		t.Error("expected FeedbackRelayed to be false by default")
	}
}
