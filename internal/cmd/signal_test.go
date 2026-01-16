package cmd

import (
	"testing"
)

func TestResolveNudgePattern(t *testing.T) {
	// Create test agent sessions (warchief/shaman use hq- prefix)
	agents := []*AgentSession{
		{Name: "hq-warchief", Type: AgentWarchief},
		{Name: "hq-shaman", Type: AgentShaman},
		{Name: "hd-horde-witness", Type: AgentWitness, Warband: "horde"},
		{Name: "hd-horde-forge", Type: AgentForge, Warband: "horde"},
		{Name: "hd-horde-clan-max", Type: AgentCrew, Warband: "horde", AgentName: "max"},
		{Name: "hd-horde-clan-jack", Type: AgentCrew, Warband: "horde", AgentName: "jack"},
		{Name: "hd-horde-alpha", Type: AgentRaider, Warband: "horde", AgentName: "alpha"},
		{Name: "hd-horde-beta", Type: AgentRaider, Warband: "horde", AgentName: "beta"},
		{Name: "hd-relics-witness", Type: AgentWitness, Warband: "relics"},
		{Name: "hd-relics-gamma", Type: AgentRaider, Warband: "relics", AgentName: "gamma"},
	}

	tests := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{
			name:     "warchief special case",
			pattern:  "warchief",
			expected: []string{"hq-warchief"},
		},
		{
			name:     "shaman special case",
			pattern:  "shaman",
			expected: []string{"hq-shaman"},
		},
		{
			name:     "specific witness",
			pattern:  "horde/witness",
			expected: []string{"hd-horde-witness"},
		},
		{
			name:     "all witnesses",
			pattern:  "*/witness",
			expected: []string{"hd-horde-witness", "hd-relics-witness"},
		},
		{
			name:     "specific forge",
			pattern:  "horde/forge",
			expected: []string{"hd-horde-forge"},
		},
		{
			name:     "all raiders in warband",
			pattern:  "horde/raiders/*",
			expected: []string{"hd-horde-alpha", "hd-horde-beta"},
		},
		{
			name:     "specific raider",
			pattern:  "horde/raiders/alpha",
			expected: []string{"hd-horde-alpha"},
		},
		{
			name:     "all clan in warband",
			pattern:  "horde/clan/*",
			expected: []string{"hd-horde-clan-max", "hd-horde-clan-jack"},
		},
		{
			name:     "specific clan member",
			pattern:  "horde/clan/max",
			expected: []string{"hd-horde-clan-max"},
		},
		{
			name:     "legacy raider format",
			pattern:  "horde/alpha",
			expected: []string{"hd-horde-alpha"},
		},
		{
			name:     "no matches",
			pattern:  "nonexistent/raiders/*",
			expected: nil,
		},
		{
			name:     "invalid pattern",
			pattern:  "invalid",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveNudgePattern(tt.pattern, agents)

			if len(got) != len(tt.expected) {
				t.Errorf("resolveNudgePattern(%q) returned %d results, want %d: got %v, want %v",
					tt.pattern, len(got), len(tt.expected), got, tt.expected)
				return
			}

			// Check each expected value is present
			gotMap := make(map[string]bool)
			for _, g := range got {
				gotMap[g] = true
			}
			for _, e := range tt.expected {
				if !gotMap[e] {
					t.Errorf("resolveNudgePattern(%q) missing expected %q, got %v",
						tt.pattern, e, got)
				}
			}
		})
	}
}
