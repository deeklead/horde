package witness

import (
	"strings"
	"testing"

	"github.com/deeklead/horde/internal/relics"
)

func TestBuildWitnessStartCommand_UsesRoleConfig(t *testing.T) {
	roleConfig := &relics.RoleConfig{
		StartCommand: "exec run --encampment {encampment} --warband {warband} --role {role}",
	}

	got, err := buildWitnessStartCommand("/encampment/warband", "horde", "/encampment", "", roleConfig)
	if err != nil {
		t.Fatalf("buildWitnessStartCommand: %v", err)
	}

	want := "exec run --encampment /encampment --warband horde --role witness"
	if got != want {
		t.Errorf("buildWitnessStartCommand = %q, want %q", got, want)
	}
}

func TestBuildWitnessStartCommand_DefaultsToRuntime(t *testing.T) {
	got, err := buildWitnessStartCommand("/encampment/warband", "horde", "/encampment", "", nil)
	if err != nil {
		t.Fatalf("buildWitnessStartCommand: %v", err)
	}

	if !strings.Contains(got, "GT_ROLE=witness") {
		t.Errorf("expected GT_ROLE=witness in command, got %q", got)
	}
	if !strings.Contains(got, "BD_ACTOR=horde/witness") {
		t.Errorf("expected BD_ACTOR=horde/witness in command, got %q", got)
	}
}

func TestBuildWitnessStartCommand_AgentOverrideWins(t *testing.T) {
	roleConfig := &relics.RoleConfig{
		StartCommand: "exec run --role {role}",
	}

	got, err := buildWitnessStartCommand("/encampment/warband", "horde", "/encampment", "codex", roleConfig)
	if err != nil {
		t.Fatalf("buildWitnessStartCommand: %v", err)
	}
	if strings.Contains(got, "exec run") {
		t.Fatalf("expected agent override to bypass role start_command, got %q", got)
	}
	if !strings.Contains(got, "GT_ROLE=witness") {
		t.Errorf("expected GT_ROLE=witness in command, got %q", got)
	}
}
