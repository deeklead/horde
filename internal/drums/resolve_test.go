package drums

import (
	"testing"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		pattern string
		address string
		want    bool
	}{
		// Exact matches
		{"horde/witness", "horde/witness", true},
		{"warchief/", "warchief/", true},

		// Wildcard matches
		{"*/witness", "horde/witness", true},
		{"*/witness", "relics/witness", true},
		{"horde/*", "horde/witness", true},
		{"horde/*", "horde/forge", true},
		{"horde/clan/*", "horde/clan/max", true},

		// Non-matches
		{"*/witness", "horde/forge", false},
		{"horde/*", "relics/witness", false},
		{"horde/clan/*", "horde/raiders/Toast", false},

		// Different path lengths
		{"horde/*", "horde/clan/max", false},      // * matches single segment
		{"horde/*/*", "horde/clan/max", true},     // Multiple wildcards
		{"*/*", "horde/witness", true},              // Both wildcards
		{"*/*/*", "horde/clan/max", true},           // Three-level wildcard
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.address, func(t *testing.T) {
			got := matchPattern(tt.pattern, tt.address)
			if got != tt.want {
				t.Errorf("matchPattern(%q, %q) = %v, want %v", tt.pattern, tt.address, got, tt.want)
			}
		})
	}
}

func TestAgentBeadIDToAddress(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		// Encampment-level agents
		{"gt-warchief", "warchief/"},
		{"gt-shaman", "shaman/"},

		// Warband singletons
		{"gt-horde-witness", "horde/witness"},
		{"gt-horde-forge", "horde/forge"},
		{"gt-relics-witness", "relics/witness"},

		// Named agents
		{"gt-horde-clan-max", "horde/clan/max"},
		{"gt-horde-raider-Toast", "horde/raider/Toast"},
		{"gt-relics-clan-wolf", "relics/clan/wolf"},

		// Agent with hyphen in name
		{"gt-horde-clan-max-v2", "horde/clan/max-v2"},
		{"gt-horde-raider-my-agent", "horde/raider/my-agent"},

		// Invalid
		{"invalid", ""},
		{"not-gt-prefix", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			got := agentBeadIDToAddress(tt.id)
			if got != tt.want {
				t.Errorf("agentBeadIDToAddress(%q) = %q, want %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestResolverResolve_DirectAddresses(t *testing.T) {
	resolver := NewResolver(nil, "")

	tests := []struct {
		name    string
		address string
		want    RecipientType
		wantLen int
	}{
		// Direct agent addresses
		{"direct agent", "horde/witness", RecipientAgent, 1},
		{"direct clan", "horde/clan/max", RecipientAgent, 1},
		{"warchief", "warchief/", RecipientAgent, 1},

		// Legacy prefixes (pass-through)
		{"list prefix", "list:oncall", RecipientAgent, 1},
		{"announce prefix", "announce:alerts", RecipientAgent, 1},

		// Explicit type prefixes
		{"queue prefix", "queue:work", RecipientQueue, 1},
		{"channel prefix", "channel:alerts", RecipientChannel, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolver.Resolve(tt.address)
			if err != nil {
				t.Fatalf("Resolve(%q) error: %v", tt.address, err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("Resolve(%q) returned %d recipients, want %d", tt.address, len(got), tt.wantLen)
			}
			if len(got) > 0 && got[0].Type != tt.want {
				t.Errorf("Resolve(%q)[0].Type = %v, want %v", tt.address, got[0].Type, tt.want)
			}
		})
	}
}

func TestResolverResolve_AtPatterns(t *testing.T) {
	// Without relics, @patterns are passed through for existing router
	resolver := NewResolver(nil, "")

	tests := []struct {
		address string
	}{
		{"@encampment"},
		{"@witnesses"},
		{"@warband/horde"},
		{"@overseer"},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got, err := resolver.Resolve(tt.address)
			if err != nil {
				t.Fatalf("Resolve(%q) error: %v", tt.address, err)
			}
			if len(got) != 1 {
				t.Errorf("Resolve(%q) returned %d recipients, want 1", tt.address, len(got))
			}
			// Without relics, @patterns pass through unchanged
			if got[0].Address != tt.address {
				t.Errorf("Resolve(%q) = %q, want pass-through", tt.address, got[0].Address)
			}
		})
	}
}

func TestResolverResolve_UnknownName(t *testing.T) {
	resolver := NewResolver(nil, "")

	// A bare name without prefix should fail if not found
	_, err := resolver.Resolve("unknown-name")
	if err == nil {
		t.Error("Resolve(\"unknown-name\") should return error for unknown name")
	}
}
