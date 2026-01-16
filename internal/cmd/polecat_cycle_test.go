package cmd

import "testing"

func TestParseRaiderSessionName(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		wantRig     string
		wantRaider string
		wantOk      bool
	}{
		// Valid raider sessions
		{
			name:        "simple raider",
			sessionName: "hd-greenplace-Toast",
			wantRig:     "greenplace",
			wantRaider: "Toast",
			wantOk:      true,
		},
		{
			name:        "another raider",
			sessionName: "hd-greenplace-Nux",
			wantRig:     "greenplace",
			wantRaider: "Nux",
			wantOk:      true,
		},
		{
			name:        "raider in different warband",
			sessionName: "hd-relics-Worker",
			wantRig:     "relics",
			wantRaider: "Worker",
			wantOk:      true,
		},
		{
			name:        "raider with hyphen in name",
			sessionName: "hd-greenplace-Max-01",
			wantRig:     "greenplace",
			wantRaider: "Max-01",
			wantOk:      true,
		},

		// Not raider sessions (should return false)
		{
			name:        "clan session",
			sessionName: "hd-greenplace-clan-jack",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "witness session",
			sessionName: "hd-greenplace-witness",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "forge session",
			sessionName: "hd-greenplace-forge",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "warchief session",
			sessionName: "hd-ai-warchief",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "shaman session",
			sessionName: "hd-ai-shaman",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "no hd prefix",
			sessionName: "horde-Toast",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "empty string",
			sessionName: "",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "just hd prefix",
			sessionName: "hd-",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
		{
			name:        "no name after warband",
			sessionName: "hd-greenplace-",
			wantRig:     "",
			wantRaider: "",
			wantOk:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRig, gotRaider, gotOk := parseRaiderSessionName(tt.sessionName)
			if gotRig != tt.wantRig || gotRaider != tt.wantRaider || gotOk != tt.wantOk {
				t.Errorf("parseRaiderSessionName(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.sessionName, gotRig, gotRaider, gotOk, tt.wantRig, tt.wantRaider, tt.wantOk)
			}
		})
	}
}
