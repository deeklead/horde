package cmd

import (
	"strings"
	"testing"
)

func TestForgeStartAgentFlag(t *testing.T) {
	flag := forgeStartCmd.Flags().Lookup("agent")
	if flag == nil {
		t.Fatal("expected forge start to define --agent flag")
	}
	if flag.DefValue != "" {
		t.Errorf("expected default agent override to be empty, got %q", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "overrides encampment default") {
		t.Errorf("expected --agent usage to mention overrides encampment default, got %q", flag.Usage)
	}
}

func TestForgeAttachAgentFlag(t *testing.T) {
	flag := forgeAttachCmd.Flags().Lookup("agent")
	if flag == nil {
		t.Fatal("expected forge summon to define --agent flag")
	}
	if flag.DefValue != "" {
		t.Errorf("expected default agent override to be empty, got %q", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "overrides encampment default") {
		t.Errorf("expected --agent usage to mention overrides encampment default, got %q", flag.Usage)
	}
}

func TestForgeRestartAgentFlag(t *testing.T) {
	flag := forgeRestartCmd.Flags().Lookup("agent")
	if flag == nil {
		t.Fatal("expected forge restart to define --agent flag")
	}
	if flag.DefValue != "" {
		t.Errorf("expected default agent override to be empty, got %q", flag.DefValue)
	}
	if !strings.Contains(flag.Usage, "overrides encampment default") {
		t.Errorf("expected --agent usage to mention overrides encampment default, got %q", flag.Usage)
	}
}
