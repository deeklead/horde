package cmd

import "testing"

func TestIdentityToEmail(t *testing.T) {
	tests := []struct {
		name     string
		identity string
		domain   string
		want     string
	}{
		{
			name:     "clan member",
			identity: "horde/clan/jack",
			domain:   "horde.local",
			want:     "horde.clan.jack@horde.local",
		},
		{
			name:     "raider",
			identity: "horde/raiders/max",
			domain:   "horde.local",
			want:     "horde.raiders.max@horde.local",
		},
		{
			name:     "witness",
			identity: "horde/witness",
			domain:   "horde.local",
			want:     "horde.witness@horde.local",
		},
		{
			name:     "forge",
			identity: "horde/forge",
			domain:   "horde.local",
			want:     "horde.forge@horde.local",
		},
		{
			name:     "warchief with trailing slash",
			identity: "warchief/",
			domain:   "horde.local",
			want:     "warchief@horde.local",
		},
		{
			name:     "shaman with trailing slash",
			identity: "shaman/",
			domain:   "horde.local",
			want:     "shaman@horde.local",
		},
		{
			name:     "custom domain",
			identity: "myrig/clan/alice",
			domain:   "example.com",
			want:     "myrig.clan.alice@example.com",
		},
		{
			name:     "deeply nested",
			identity: "warband/raiders/nested/deep",
			domain:   "test.io",
			want:     "warband.raiders.nested.deep@test.io",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := identityToEmail(tt.identity, tt.domain)
			if got != tt.want {
				t.Errorf("identityToEmail(%q, %q) = %q, want %q",
					tt.identity, tt.domain, got, tt.want)
			}
		})
	}
}
