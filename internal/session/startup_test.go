package session

import (
	"strings"
	"testing"
)

func TestFormatStartupNudge(t *testing.T) {
	tests := []struct {
		name     string
		cfg      StartupNudgeConfig
		wantSub  []string // substrings that must appear
		wantNot  []string // substrings that must NOT appear
	}{
		{
			name: "assigned with totem-id",
			cfg: StartupNudgeConfig{
				Recipient: "horde/clan/gus",
				Sender:    "shaman",
				Topic:     "assigned",
				MolID:     "gt-abc12",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"horde/clan/gus",
				"<- shaman",
				"assigned:gt-abc12",
				"Work is on your hook", // assigned includes actionable instructions
				"hd hook",
			},
		},
		{
			name: "cold-start no totem-id",
			cfg: StartupNudgeConfig{
				Recipient: "shaman",
				Sender:    "warchief",
				Topic:     "cold-start",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"shaman",
				"<- warchief",
				"cold-start",
				"Check your hook and drums", // cold-start includes explicit instructions (like handoff)
				"hd hook",
				"hd drums inbox",
			},
			// No wantNot - timestamp contains ":"
		},
		{
			name: "handoff self",
			cfg: StartupNudgeConfig{
				Recipient: "horde/witness",
				Sender:    "self",
				Topic:     "handoff",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"horde/witness",
				"<- self",
				"handoff",
				"Check your hook and drums", // handoff includes explicit instructions
				"hd hook",
				"hd drums inbox",
			},
		},
		{
			name: "totem-id only",
			cfg: StartupNudgeConfig{
				Recipient: "horde/raiders/Toast",
				Sender:    "witness",
				MolID:     "gt-xyz99",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"horde/raiders/Toast",
				"<- witness",
				"gt-xyz99",
			},
		},
		{
			name: "empty topic defaults to ready",
			cfg: StartupNudgeConfig{
				Recipient: "shaman",
				Sender:    "warchief",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"ready",
			},
		},
		{
			name: "start includes fallback instructions",
			cfg: StartupNudgeConfig{
				Recipient: "relics/clan/fang",
				Sender:    "human",
				Topic:     "start",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"relics/clan/fang",
				"<- human",
				"start",
				"hd rally", // fallback instruction for when SessionStart hook fails
			},
		},
		{
			name: "restart includes fallback instructions",
			cfg: StartupNudgeConfig{
				Recipient: "horde/clan/george",
				Sender:    "human",
				Topic:     "restart",
			},
			wantSub: []string{
				"[GAS ENCAMPMENT]",
				"horde/clan/george",
				"restart",
				"hd rally", // fallback instruction for when SessionStart hook fails
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatStartupNudge(tt.cfg)

			for _, sub := range tt.wantSub {
				if !strings.Contains(got, sub) {
					t.Errorf("FormatStartupNudge() = %q, want to contain %q", got, sub)
				}
			}

			for _, sub := range tt.wantNot {
				if strings.Contains(got, sub) {
					t.Errorf("FormatStartupNudge() = %q, should NOT contain %q", got, sub)
				}
			}
		})
	}
}
