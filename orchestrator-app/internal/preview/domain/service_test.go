package domain

import "testing"

func TestValidateTransition(t *testing.T) {
	tests := []struct {
		name    string
		from    Status
		to      Status
		wantErr bool
	}{
		{"pending to building", StatusPending, StatusBuilding, false},
		{"building to deploying", StatusBuilding, StatusDeploying, false},
		{"building to failed", StatusBuilding, StatusFailed, false},
		{"deploying to ready", StatusDeploying, StatusReady, false},
		{"deploying to failed", StatusDeploying, StatusFailed, false},
		{"ready to pending (rebuild)", StatusReady, StatusPending, false},
		{"ready to deleted", StatusReady, StatusDeleted, false},
		{"failed to pending (retry)", StatusFailed, StatusPending, false},
		{"failed to deleted", StatusFailed, StatusDeleted, false},

		// Invalid transitions
		{"pending to ready", StatusPending, StatusReady, true},
		{"pending to deleted", StatusPending, StatusDeleted, true},
		{"building to ready", StatusBuilding, StatusReady, true},
		{"ready to building", StatusReady, StatusBuilding, true},
		{"deleted to anything", StatusDeleted, StatusPending, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTransition(tt.from, tt.to)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateTransition(%q, %q) error = %v, wantErr %v", tt.from, tt.to, err, tt.wantErr)
			}
		})
	}
}
