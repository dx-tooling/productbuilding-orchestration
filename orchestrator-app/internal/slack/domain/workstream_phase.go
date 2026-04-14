package domain

// WorkstreamPhase represents the lifecycle stage of a workstream.
type WorkstreamPhase string

const (
	PhaseIntake     WorkstreamPhase = "intake"
	PhaseOpen       WorkstreamPhase = "open"
	PhaseInProgress WorkstreamPhase = "in-progress"
	PhaseReview     WorkstreamPhase = "review"
	PhaseRevision   WorkstreamPhase = "revision"
	PhaseDone       WorkstreamPhase = "done"
	PhaseAbandoned  WorkstreamPhase = "abandoned"
)

var validPhases = map[WorkstreamPhase]bool{
	PhaseIntake:     true,
	PhaseOpen:       true,
	PhaseInProgress: true,
	PhaseReview:     true,
	PhaseRevision:   true,
	PhaseDone:       true,
	PhaseAbandoned:  true,
}

// IsValid returns true if the phase is a recognized workstream phase.
func (p WorkstreamPhase) IsValid() bool {
	return validPhases[p]
}
