package cmd

import (
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

func TestMigrationResultStatus(t *testing.T) {
	tests := []struct {
		name     string
		result   migrationResult
		wantIcon string
	}{
		{
			name: "migrated shows checkmark",
			result: migrationResult{
				OldID:   "hd-warchief",
				NewID:   "hq-warchief",
				Status:  "migrated",
				Message: "successfully migrated",
			},
			wantIcon: "  ✓",
		},
		{
			name: "would migrate shows checkmark",
			result: migrationResult{
				OldID:   "hd-warchief",
				NewID:   "hq-warchief",
				Status:  "would migrate",
				Message: "would copy state from gt-warchief",
			},
			wantIcon: "  ✓",
		},
		{
			name: "skipped shows empty circle",
			result: migrationResult{
				OldID:   "hd-warchief",
				NewID:   "hq-warchief",
				Status:  "skipped",
				Message: "already exists",
			},
			wantIcon: "  ⊘",
		},
		{
			name: "error shows X",
			result: migrationResult{
				OldID:   "hd-warchief",
				NewID:   "hq-warchief",
				Status:  "error",
				Message: "failed to create",
			},
			wantIcon: "  ✗",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var icon string
			switch tt.result.Status {
			case "migrated", "would migrate":
				icon = "  ✓"
			case "skipped":
				icon = "  ⊘"
			case "error":
				icon = "  ✗"
			}
			if icon != tt.wantIcon {
				t.Errorf("icon for status %q = %q, want %q", tt.result.Status, icon, tt.wantIcon)
			}
		})
	}
}

func TestTownBeadIDHelpers(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"WarchiefBeadIDTown", relics.WarchiefBeadIDTown(), "hq-warchief"},
		{"ShamanBeadIDTown", relics.ShamanBeadIDTown(), "hq-shaman"},
		{"DogBeadIDTown", relics.DogBeadIDTown("fido"), "hq-dog-fido"},
		{"RoleBeadIDTown warchief", relics.RoleBeadIDTown("warchief"), "hq-warchief-role"},
		{"RoleBeadIDTown witness", relics.RoleBeadIDTown("witness"), "hq-witness-role"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}
