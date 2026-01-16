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
				"GT_ROLE":    "raider",
				"GT_RIG":     "horde",
				"GT_RAIDER": "toast",
			},
			expected: "gt-horde-toast",
		},
		{
			name: "clan session",
			envVars: map[string]string{
				"GT_ROLE": "clan",
				"GT_RIG":  "horde",
				"GT_CREW": "max",
			},
			expected: "gt-horde-clan-max",
		},
		{
			name: "witness session",
			envVars: map[string]string{
				"GT_ROLE": "witness",
				"GT_RIG":  "horde",
			},
			expected: "gt-horde-witness",
		},
		{
			name: "forge session",
			envVars: map[string]string{
				"GT_ROLE": "forge",
				"GT_RIG":  "horde",
			},
			expected: "gt-horde-forge",
		},
		{
			name: "warchief session",
			envVars: map[string]string{
				"GT_ROLE": "warchief",
				"GT_TOWN": "ai",
			},
			expected: "gt-ai-warchief",
		},
		{
			name: "shaman session",
			envVars: map[string]string{
				"GT_ROLE": "shaman",
				"GT_TOWN": "ai",
			},
			expected: "gt-ai-shaman",
		},
		{
			name: "warchief session without GT_TOWN",
			envVars: map[string]string{
				"GT_ROLE": "warchief",
			},
			expected: "gt-warchief",
		},
		{
			name: "shaman session without GT_TOWN",
			envVars: map[string]string{
				"GT_ROLE": "shaman",
			},
			expected: "gt-shaman",
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
			envKeys := []string{"GT_ROLE", "GT_RIG", "GT_RAIDER", "GT_CREW", "GT_TOWN"}
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
