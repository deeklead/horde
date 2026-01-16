package relics

import "testing"

// TestWarchiefBeadIDTown tests the encampment-level Warchief bead ID.
func TestWarchiefBeadIDTown(t *testing.T) {
	got := WarchiefBeadIDTown()
	want := "hq-warchief"
	if got != want {
		t.Errorf("WarchiefBeadIDTown() = %q, want %q", got, want)
	}
}

// TestShamanBeadIDTown tests the encampment-level Shaman bead ID.
func TestShamanBeadIDTown(t *testing.T) {
	got := ShamanBeadIDTown()
	want := "hq-shaman"
	if got != want {
		t.Errorf("ShamanBeadIDTown() = %q, want %q", got, want)
	}
}

// TestDogBeadIDTown tests encampment-level Dog bead IDs.
func TestDogBeadIDTown(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"alpha", "hq-dog-alpha"},
		{"rex", "hq-dog-rex"},
		{"spot", "hq-dog-spot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DogBeadIDTown(tt.name)
			if got != tt.want {
				t.Errorf("DogBeadIDTown(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestRoleBeadIDTown tests encampment-level role bead IDs.
func TestRoleBeadIDTown(t *testing.T) {
	tests := []struct {
		roleType string
		want     string
	}{
		{"warchief", "hq-warchief-role"},
		{"shaman", "hq-shaman-role"},
		{"dog", "hq-dog-role"},
		{"witness", "hq-witness-role"},
	}

	for _, tt := range tests {
		t.Run(tt.roleType, func(t *testing.T) {
			got := RoleBeadIDTown(tt.roleType)
			if got != tt.want {
				t.Errorf("RoleBeadIDTown(%q) = %q, want %q", tt.roleType, got, tt.want)
			}
		})
	}
}

// TestWarchiefRoleBeadIDTown tests the Warchief role bead ID for encampment-level.
func TestWarchiefRoleBeadIDTown(t *testing.T) {
	got := WarchiefRoleBeadIDTown()
	want := "hq-warchief-role"
	if got != want {
		t.Errorf("WarchiefRoleBeadIDTown() = %q, want %q", got, want)
	}
}

// TestShamanRoleBeadIDTown tests the Shaman role bead ID for encampment-level.
func TestShamanRoleBeadIDTown(t *testing.T) {
	got := ShamanRoleBeadIDTown()
	want := "hq-shaman-role"
	if got != want {
		t.Errorf("ShamanRoleBeadIDTown() = %q, want %q", got, want)
	}
}

// TestDogRoleBeadIDTown tests the Dog role bead ID for encampment-level.
func TestDogRoleBeadIDTown(t *testing.T) {
	got := DogRoleBeadIDTown()
	want := "hq-dog-role"
	if got != want {
		t.Errorf("DogRoleBeadIDTown() = %q, want %q", got, want)
	}
}
