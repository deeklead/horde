package cmd

import (
	"os"
	"testing"
)

func TestDeriveSessionName(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected string
	}{
		{
			name: "raider session",
			envVars: map[string]string{
				"HD_ROLE":    "raider",
				"HD_WARBAND":     "horde",
				"HD_RAIDER": "toast",
			},
			expected: "hd-horde-toast",
		},
		{
			name: "clan session",
			envVars: map[string]string{
				"HD_ROLE": "clan",
				"HD_WARBAND":  "horde",
				"HD_CLAN": "max",
			},
			expected: "hd-horde-clan-max",
		},
		{
			name: "witness session",
			envVars: map[string]string{
				"HD_ROLE": "witness",
				"HD_WARBAND":  "horde",
			},
			expected: "hd-horde-witness",
		},
		{
			name: "forge session",
			envVars: map[string]string{
				"HD_ROLE": "forge",
				"HD_WARBAND":  "horde",
			},
			expected: "hd-horde-forge",
		},
		{
			name: "warchief session",
			envVars: map[string]string{
				"HD_ROLE": "warchief",
				"HD_ENCAMPMENT": "ai",
			},
			expected: "hd-ai-warchief",
		},
		{
			name: "shaman session",
			envVars: map[string]string{
				"HD_ROLE": "shaman",
				"HD_ENCAMPMENT": "ai",
			},
			expected: "hd-ai-shaman",
		},
		{
			name: "warchief session without HD_ENCAMPMENT",
			envVars: map[string]string{
				"HD_ROLE": "warchief",
			},
			expected: "hd-warchief",
		},
		{
			name: "shaman session without HD_ENCAMPMENT",
			envVars: map[string]string{
				"HD_ROLE": "shaman",
			},
			expected: "hd-shaman",
		},
		{
			name:     "no env vars",
			envVars:  map[string]string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and clear relevant env vars
			saved := make(map[string]string)
			envKeys := []string{"HD_ROLE", "HD_WARBAND", "HD_RAIDER", "HD_CLAN", "HD_ENCAMPMENT"}
			for _, key := range envKeys {
				saved[key] = os.Getenv(key)
				os.Unsetenv(key)
			}
			defer func() {
				// Restore env vars
				for key, val := range saved {
					if val != "" {
						os.Setenv(key, val)
					}
				}
			}()

			// Set test env vars
			for key, val := range tt.envVars {
				os.Setenv(key, val)
			}

			result := deriveSessionName()
			if result != tt.expected {
				t.Errorf("deriveSessionName() = %q, want %q", result, tt.expected)
			}
		})
	}
}
