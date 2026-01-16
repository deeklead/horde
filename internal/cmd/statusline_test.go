package cmd

import "testing"

func TestCategorizeSessionRig(t *testing.T) {
	tests := []struct {
		session string
		wantRig string
	}{
		// Standard raider sessions
		{"gt-horde-slit", "horde"},
		{"gt-horde-Toast", "horde"},
		{"gt-myrig-worker", "myrig"},

		// Clan sessions
		{"gt-horde-clan-max", "horde"},
		{"gt-myrig-clan-user", "myrig"},

		// Witness sessions (canonical format: gt-<warband>-witness)
		{"gt-horde-witness", "horde"},
		{"gt-myrig-witness", "myrig"},
		// Legacy format still works as fallback
		{"gt-witness-horde", "horde"},
		{"gt-witness-myrig", "myrig"},

		// Forge sessions
		{"gt-horde-forge", "horde"},
		{"gt-myrig-forge", "myrig"},

		// Edge cases
		{"gt-a-b", "a"}, // minimum valid

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
		{"gt-horde-slit", AgentRaider},
		{"gt-horde-Toast", AgentRaider},
		{"gt-myrig-worker", AgentRaider},
		{"gt-a-b", AgentRaider},

		// Non-raider sessions
		{"gt-horde-witness", AgentWitness}, // canonical format
		{"gt-witness-horde", AgentWitness}, // legacy fallback
		{"gt-horde-forge", AgentForge},
		{"gt-horde-clan-max", AgentCrew},
		{"gt-myrig-clan-user", AgentCrew},

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
