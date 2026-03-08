package domain

import "fmt"

// validTransitions defines the allowed state transitions.
var validTransitions = map[Status][]Status{
	StatusPending:   {StatusBuilding},
	StatusBuilding:  {StatusDeploying, StatusFailed},
	StatusDeploying: {StatusReady, StatusFailed},
	StatusReady:     {StatusPending, StatusDeleted},
	StatusFailed:    {StatusPending, StatusDeleted},
}

// ValidateTransition checks if a state transition is allowed.
func ValidateTransition(from, to Status) error {
	allowed, ok := validTransitions[from]
	if !ok {
		return fmt.Errorf("no transitions defined from state %q", from)
	}
	for _, s := range allowed {
		if s == to {
			return nil
		}
	}
	return fmt.Errorf("invalid transition from %q to %q", from, to)
}
