package session

import (
	"testing"
)

func TestParseSessionName(t *testing.T) {
	tests := []struct {
		name     string
		session  string
		wantRole Role
		wantRig  string
		wantName string
		wantErr  bool
	}{
		// Encampment-level roles (hq-warchief, hq-shaman)
		{
			name:     "warchief",
			session:  "hq-warchief",
			wantRole: RoleWarchief,
		},
		{
			name:     "shaman",
			session:  "hq-shaman",
			wantRole: RoleShaman,
		},

		// Witness (simple warband)
		{
			name:     "witness simple warband",
			session:  "hd-horde-witness",
			wantRole: RoleWitness,
			wantRig:  "horde",
		},
		{
			name:     "witness hyphenated warband",
			session:  "hd-foo-bar-witness",
			wantRole: RoleWitness,
			wantRig:  "foo-bar",
		},

		// Forge (simple warband)
		{
			name:     "forge simple warband",
			session:  "hd-horde-forge",
			wantRole: RoleForge,
			wantRig:  "horde",
		},
		{
			name:     "forge hyphenated warband",
			session:  "hd-my-project-forge",
			wantRole: RoleForge,
			wantRig:  "my-project",
		},

		// Clan (with marker)
		{
			name:     "clan simple",
			session:  "hd-horde-clan-max",
			wantRole: RoleCrew,
			wantRig:  "horde",
			wantName: "max",
		},
		{
			name:     "clan hyphenated warband",
			session:  "hd-foo-bar-clan-alice",
			wantRole: RoleCrew,
			wantRig:  "foo-bar",
			wantName: "alice",
		},
		{
			name:     "clan hyphenated name",
			session:  "hd-horde-clan-my-worker",
			wantRole: RoleCrew,
			wantRig:  "horde",
			wantName: "my-worker",
		},

		// Raider (fallback)
		{
			name:     "raider simple",
			session:  "hd-horde-morsov",
			wantRole: RoleRaider,
			wantRig:  "horde",
			wantName: "morsov",
		},
		{
			name:     "raider hyphenated warband",
			session:  "hd-foo-bar-Toast",
			wantRole: RoleRaider,
			wantRig:  "foo-bar",
			wantName: "Toast",
		},

		// Error cases
		{
			name:    "missing prefix",
			session: "horde-witness",
			wantErr: true,
		},
		{
			name:    "empty after prefix",
			session: "hd-",
			wantErr: true,
		},
		{
			name:    "just prefix single segment",
			session: "hd-x",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSessionName(tt.session)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSessionName(%q) error = %v, wantErr %v", tt.session, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.Role != tt.wantRole {
				t.Errorf("ParseSessionName(%q).Role = %v, want %v", tt.session, got.Role, tt.wantRole)
			}
			if got.Warband != tt.wantRig {
				t.Errorf("ParseSessionName(%q).Warband = %v, want %v", tt.session, got.Warband, tt.wantRig)
			}
			if got.Name != tt.wantName {
				t.Errorf("ParseSessionName(%q).Name = %v, want %v", tt.session, got.Name, tt.wantName)
			}
		})
	}
}

func TestAgentIdentity_SessionName(t *testing.T) {
	tests := []struct {
		name     string
		identity AgentIdentity
		want     string
	}{
		{
			name:     "warchief",
			identity: AgentIdentity{Role: RoleWarchief},
			want:     "hq-warchief",
		},
		{
			name:     "shaman",
			identity: AgentIdentity{Role: RoleShaman},
			want:     "hq-shaman",
		},
		{
			name:     "witness",
			identity: AgentIdentity{Role: RoleWitness, Warband: "horde"},
			want:     "hd-horde-witness",
		},
		{
			name:     "forge",
			identity: AgentIdentity{Role: RoleForge, Warband: "my-project"},
			want:     "hd-my-project-forge",
		},
		{
			name:     "clan",
			identity: AgentIdentity{Role: RoleCrew, Warband: "horde", Name: "max"},
			want:     "hd-horde-clan-max",
		},
		{
			name:     "raider",
			identity: AgentIdentity{Role: RoleRaider, Warband: "horde", Name: "morsov"},
			want:     "hd-horde-morsov",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.identity.SessionName(); got != tt.want {
				t.Errorf("AgentIdentity.SessionName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentIdentity_Address(t *testing.T) {
	tests := []struct {
		name     string
		identity AgentIdentity
		want     string
	}{
		{
			name:     "warchief",
			identity: AgentIdentity{Role: RoleWarchief},
			want:     "warchief",
		},
		{
			name:     "shaman",
			identity: AgentIdentity{Role: RoleShaman},
			want:     "shaman",
		},
		{
			name:     "witness",
			identity: AgentIdentity{Role: RoleWitness, Warband: "horde"},
			want:     "horde/witness",
		},
		{
			name:     "forge",
			identity: AgentIdentity{Role: RoleForge, Warband: "my-project"},
			want:     "my-project/forge",
		},
		{
			name:     "clan",
			identity: AgentIdentity{Role: RoleCrew, Warband: "horde", Name: "max"},
			want:     "horde/clan/max",
		},
		{
			name:     "raider",
			identity: AgentIdentity{Role: RoleRaider, Warband: "horde", Name: "Toast"},
			want:     "horde/raiders/Toast",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.identity.Address(); got != tt.want {
				t.Errorf("AgentIdentity.Address() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSessionName_RoundTrip(t *testing.T) {
	// Test that parsing then reconstructing gives the same result
	sessions := []string{
		"hq-warchief",
		"hq-shaman",
		"hd-horde-witness",
		"hd-foo-bar-forge",
		"hd-horde-clan-max",
		"hd-horde-morsov",
	}

	for _, sess := range sessions {
		t.Run(sess, func(t *testing.T) {
			identity, err := ParseSessionName(sess)
			if err != nil {
				t.Fatalf("ParseSessionName(%q) error = %v", sess, err)
			}
			if got := identity.SessionName(); got != sess {
				t.Errorf("Round-trip failed: ParseSessionName(%q).SessionName() = %q", sess, got)
			}
		})
	}
}
