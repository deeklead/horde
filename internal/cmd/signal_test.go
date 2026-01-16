package cmd

import (
	"testing"
)

func TestResolveNudgePattern(t *testing.T) {
	// Create test agent sessions (warchief/shaman use hq- prefix)
	agents := []*AgentSession{
		{Name: "hq-warchief", Type: AgentWarchief},
		{Name: "hq-shaman", Type: AgentShaman},
		{Name: "gt-horde-witness", Type: AgentWitness, Warband: "horde"},
		{Name: "gt-horde-forge", Type: AgentForge, Warband: "horde"},
		{Name: "gt-horde-clan-max", Type: AgentCrew, Warband: "horde", AgentName: "max"},
		{Name: "gt-horde-clan-jack", Type: AgentCrew, Warband: "horde", AgentName: "jack"},
		{Name: "gt-horde-alpha", Type: AgentRaider, Warband: "horde", AgentName: "alpha"},
		{Name: "gt-horde-beta", Type: AgentRaider, Warband: "horde", AgentName: "beta"},
		{Name: "gt-relics-witness", Type: AgentWitness, Warband: "relics"},
		{Name: "gt-relics-gamma", Type: AgentRaider, Warband: "relics", AgentName: "gamma"},
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
			expected: []string{"gt-horde-witness"},
		},
		{
			name:     "all witnesses",
			pattern:  "*/witness",
			expected: []string{"gt-horde-witness", "gt-relics-witness"},
		},
		{
			name:     "specific forge",
			pattern:  "horde/forge",
			expected: []string{"gt-horde-forge"},
		},
		{
			name:     "all raiders in warband",
			pattern:  "horde/raiders/*",
			expected: []string{"gt-horde-alpha", "gt-horde-beta"},
		},
		{
			name:     "specific raider",
			pattern:  "horde/raiders/alpha",
			expected: []string{"gt-horde-alpha"},
		},
		{
			name:     "all clan in warband",
			pattern:  "horde/clan/*",
			expected: []string{"gt-horde-clan-max", "gt-horde-clan-jack"},
		},
		{
			name:     "specific clan member",
			pattern:  "horde/clan/max",
			expected: []string{"gt-horde-clan-max"},
		},
		{
			name:     "legacy raider format",
			pattern:  "horde/alpha",
			expected: []string{"gt-horde-alpha"},
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
