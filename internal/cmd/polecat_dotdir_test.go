package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
)

func TestDiscoverHooksSkipsRaiderDotDirs(t *testing.T) {
	townRoot := setupTestTownForDotDir(t)
	rigPath := filepath.Join(townRoot, "horde")

	settingsPath := filepath.Join(rigPath, "raiders", ".claude", ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}

	settings := `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"Stop","command":"echo hi"}]}]}}`
	if err := os.WriteFile(settingsPath, []byte(settings), 0644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	hooks, err := discoverHooks(townRoot)
	if err != nil {
		t.Fatalf("discoverHooks: %v", err)
	}

	if len(hooks) != 0 {
		t.Fatalf("expected no hooks, got %d", len(hooks))
	}
}

func TestStartRaidersWithWorkSkipsDotDirs(t *testing.T) {
	townRoot := setupTestTownForDotDir(t)
	rigName := "horde"
	rigPath := filepath.Join(townRoot, rigName)

	addRigEntry(t, townRoot, rigName)

	if err := os.MkdirAll(filepath.Join(rigPath, "raiders", ".claude"), 0755); err != nil {
		t.Fatalf("mkdir .claude raider: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, "raiders", "toast"), 0755); err != nil {
		t.Fatalf("mkdir raider: %v", err)
	}

	binDir := t.TempDir()
	bdScript := `#!/bin/sh
if [ "$1" = "--no-daemon" ]; then
  shift
fi
cmd="$1"
case "$cmd" in
  list)
    if [ "$(basename "$PWD")" = ".claude" ]; then
      echo '[{"id":"hd-1"}]'
    else
      echo '[]'
    fi
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
`
	writeScript(t, binDir, "rl", bdScript)

	tmuxScript := `#!/bin/sh
if [ "$1" = "has-session" ]; then
  echo "tmux error" 1>&2
  exit 1
fi
exit 0
`
	writeScript(t, binDir, "tmux", tmuxScript)

	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir encampment root: %v", err)
	}

	started, errs := startRaidersWithWork(townRoot, rigName)

	if len(started) != 0 {
		t.Fatalf("expected no raiders started, got %v", started)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
}

func TestRunSessionCheckSkipsDotDirs(t *testing.T) {
	townRoot := setupTestTownForDotDir(t)
	rigName := "horde"
	rigPath := filepath.Join(townRoot, rigName)

	addRigEntry(t, townRoot, rigName)

	if err := os.MkdirAll(filepath.Join(rigPath, "raiders", ".claude"), 0755); err != nil {
		t.Fatalf("mkdir .claude raider: %v", err)
	}

	binDir := t.TempDir()
	tmuxScript := `#!/bin/sh
if [ "$1" = "has-session" ]; then
  echo "can't find session" 1>&2
  exit 1
fi
exit 0
`
	writeScript(t, binDir, "tmux", tmuxScript)
	t.Setenv("PATH", fmt.Sprintf("%s:%s", binDir, os.Getenv("PATH")))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir encampment root: %v", err)
	}

	output := captureStdout(t, func() {
		if err := runSessionCheck(&cobra.Command{}, []string{rigName}); err != nil {
			t.Fatalf("runSessionCheck: %v", err)
		}
	})

	if strings.Contains(output, ".claude") {
		t.Fatalf("expected .claude to be ignored, output:\n%s", output)
	}
}

func addRigEntry(t *testing.T, townRoot, rigName string) {
	t.Helper()

	rigsPath := filepath.Join(townRoot, "warchief", "warbands.json")
	rigsConfig, err := config.LoadRigsConfig(rigsPath)
	if err != nil {
		t.Fatalf("load warbands.json: %v", err)
	}
	if rigsConfig.Warbands == nil {
		rigsConfig.Warbands = make(map[string]config.RigEntry)
	}
	rigsConfig.Warbands[rigName] = config.RigEntry{
		GitURL:  "file:///dev/null",
		AddedAt: time.Now(),
	}
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}
}

func setupTestTownForDotDir(t *testing.T) string {
	t.Helper()

	townRoot := t.TempDir()

	warchiefDir := filepath.Join(townRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	rigsPath := filepath.Join(warchiefDir, "warbands.json")
	rigsConfig := &config.RigsConfig{
		Version: 1,
		Warbands:    make(map[string]config.RigEntry),
	}
	if err := config.SaveRigsConfig(rigsPath, rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}

	relicsDir := filepath.Join(townRoot, ".relics")
	if err := os.MkdirAll(relicsDir, 0755); err != nil {
		t.Fatalf("mkdir .relics: %v", err)
	}

	return townRoot
}

func writeScript(t *testing.T, dir, name, content string) {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
