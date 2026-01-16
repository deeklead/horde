package cmd

import (
	"testing"
)

func TestAddressToAgentBeadID(t *testing.T) {
	tests := []struct {
		address  string
		expected string
	}{
		// Warchief and shaman use hq- prefix (encampment-level)
		{"warchief", "hq-warchief"},
		{"shaman", "hq-shaman"},
		{"horde/witness", "hd-horde-witness"},
		{"horde/forge", "hd-horde-forge"},
		{"horde/alpha", "hd-horde-raider-alpha"},
		{"horde/clan/max", "hd-horde-clan-max"},
		{"relics/witness", "hd-relics-witness"},
		{"relics/beta", "hd-relics-raider-beta"},
		// Invalid addresses should return empty string
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := addressToAgentBeadID(tt.address)
			if got != tt.expected {
				t.Errorf("addressToAgentBeadID(%q) = %q, want %q", tt.address, got, tt.expected)
			}
		})
	}
}
