package warband

import (
	"testing"
)

func TestRelicsPath_AlwaysReturnsRigRoot(t *testing.T) {
	t.Parallel()

	// RelicsPath should always return the warband root path, regardless of HasWarchief.
	// The redirect system at <warband>/.relics/redirect handles finding the actual
	// relics location (either local at <warband>/.relics/ or tracked at warchief/warband/.relics/).
	//
	// This ensures:
	// 1. We don't write files to the user's repo clone (warchief/warband/)
	// 2. The redirect architecture is respected
	// 3. All code paths use the same relics resolution logic

	tests := []struct {
		name     string
		warband      Warband
		wantPath string
	}{
		{
			name: "warband with warchief only",
			warband: Warband{
				Name:     "testrig",
				Path:     "/home/user/horde/testrig",
				HasWarchief: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with witness only",
			warband: Warband{
				Name:       "testrig",
				Path:       "/home/user/horde/testrig",
				HasWitness: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with forge only",
			warband: Warband{
				Name:        "testrig",
				Path:        "/home/user/horde/testrig",
				HasForge: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with no agents",
			warband: Warband{
				Name: "testrig",
				Path: "/home/user/horde/testrig",
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with warchief and witness",
			warband: Warband{
				Name:       "testrig",
				Path:       "/home/user/horde/testrig",
				HasWarchief:   true,
				HasWitness: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with warchief and forge",
			warband: Warband{
				Name:        "testrig",
				Path:        "/home/user/horde/testrig",
				HasWarchief:    true,
				HasForge: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with witness and forge",
			warband: Warband{
				Name:        "testrig",
				Path:        "/home/user/horde/testrig",
				HasWitness:  true,
				HasForge: true,
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with all agents",
			warband: Warband{
				Name:        "fullrig",
				Path:        "/tmp/horde/fullrig",
				HasWarchief:    true,
				HasWitness:  true,
				HasForge: true,
			},
			wantPath: "/tmp/horde/fullrig",
		},
		{
			name: "warband with raiders",
			warband: Warband{
				Name:     "testrig",
				Path:     "/home/user/horde/testrig",
				HasWarchief: true,
				Raiders: []string{"raider1", "raider2"},
			},
			wantPath: "/home/user/horde/testrig",
		},
		{
			name: "warband with clan",
			warband: Warband{
				Name:     "testrig",
				Path:     "/home/user/horde/testrig",
				HasWarchief: true,
				Clan:     []string{"crew1", "crew2"},
			},
			wantPath: "/home/user/horde/testrig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.warband.RelicsPath()
			if got != tt.wantPath {
				t.Errorf("RelicsPath() = %q, want %q", got, tt.wantPath)
			}
		})
	}
}

func TestDefaultBranch_FallsBackToMain(t *testing.T) {
	t.Parallel()

	// DefaultBranch should return "main" when config cannot be loaded
	warband := Warband{
		Name: "testrig",
		Path: "/nonexistent/path",
	}

	got := warband.DefaultBranch()
	if got != "main" {
		t.Errorf("DefaultBranch() = %q, want %q", got, "main")
	}
}
