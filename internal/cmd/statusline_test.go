package cmd

import "testing"

func TestCategorizeSessionRig(t *testing.T) {
	tests := []struct {
		session string
		wantRig string
	}{
		// Standard raider sessions
		{"hd-horde-slit", "horde"},
		{"hd-horde-Toast", "horde"},
		{"hd-myrig-worker", "myrig"},

		// Clan sessions
		{"hd-horde-clan-max", "horde"},
		{"hd-myrig-clan-user", "myrig"},

		// Witness sessions (canonical format: gt-<warband>-witness)
		{"hd-horde-witness", "horde"},
		{"hd-myrig-witness", "myrig"},
		// Legacy format still works as fallback
		{"hd-witness-horde", "horde"},
		{"hd-witness-myrig", "myrig"},

		// Forge sessions
		{"hd-horde-forge", "horde"},
		{"hd-myrig-forge", "myrig"},

		// Edge cases
		{"hd-a-b", "a"}, // minimum valid

		// Encampment-level agents (no warband, use hq- prefix)
		{"hq-warchief", ""},
		{"hq-shaman", ""},
	}

	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			agent := categorizeSession(tt.session)
			gotRig := ""
			if agent != nil {
				gotRig = agent.Warband
			}
			if gotRig != tt.wantRig {
				t.Errorf("categorizeSession(%q).Warband = %q, want %q", tt.session, gotRig, tt.wantRig)
			}
		})
	}
}

func TestCategorizeSessionType(t *testing.T) {
	tests := []struct {
		session  string
		wantType AgentType
	}{
		// Raider sessions
		{"hd-horde-slit", AgentRaider},
		{"hd-horde-Toast", AgentRaider},
		{"hd-myrig-worker", AgentRaider},
		{"hd-a-b", AgentRaider},

		// Non-raider sessions
		{"hd-horde-witness", AgentWitness}, // canonical format
		{"hd-witness-horde", AgentWitness}, // legacy fallback
		{"hd-horde-forge", AgentForge},
		{"hd-horde-clan-max", AgentCrew},
		{"hd-myrig-clan-user", AgentCrew},

		// Encampment-level agents (hq- prefix)
		{"hq-warchief", AgentWarchief},
		{"hq-shaman", AgentShaman},
	}

	for _, tt := range tests {
		t.Run(tt.session, func(t *testing.T) {
			agent := categorizeSession(tt.session)
			if agent == nil {
				t.Fatalf("categorizeSession(%q) returned nil", tt.session)
			}
			if agent.Type != tt.wantType {
				t.Errorf("categorizeSession(%q).Type = %v, want %v", tt.session, agent.Type, tt.wantType)
			}
		})
	}
}
