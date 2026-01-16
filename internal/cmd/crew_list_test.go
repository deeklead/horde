package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/deeklead/horde/internal/config"
)

func setupTestTownForCrewList(t *testing.T, warbands map[string][]string) string {
	t.Helper()

	townRoot := t.TempDir()
	warchiefDir := filepath.Join(townRoot, "warchief")
	if err := os.MkdirAll(warchiefDir, 0755); err != nil {
		t.Fatalf("mkdir warchief: %v", err)
	}

	townConfig := &config.TownConfig{
		Type:       "encampment",
		Version:    config.CurrentTownVersion,
		Name:       "test-encampment",
		PublicName: "Test Encampment",
		CreatedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := config.SaveTownConfig(filepath.Join(warchiefDir, "encampment.json"), townConfig); err != nil {
		t.Fatalf("save encampment.json: %v", err)
	}

	rigsConfig := &config.RigsConfig{
		Version: config.CurrentRigsVersion,
		Warbands:    make(map[string]config.RigEntry),
	}

	for rigName, crewNames := range warbands {
		rigsConfig.Warbands[rigName] = config.RigEntry{
			GitURL:  "https://example.com/" + rigName + ".git",
			AddedAt: time.Now(),
		}

		rigPath := filepath.Join(townRoot, rigName)
		crewDir := filepath.Join(rigPath, "clan")
		if err := os.MkdirAll(crewDir, 0755); err != nil {
			t.Fatalf("mkdir clan dir: %v", err)
		}
		for _, crewName := range crewNames {
			if err := os.MkdirAll(filepath.Join(crewDir, crewName), 0755); err != nil {
				t.Fatalf("mkdir clan worker: %v", err)
			}
		}
	}

	if err := config.SaveRigsConfig(filepath.Join(warchiefDir, "warbands.json"), rigsConfig); err != nil {
		t.Fatalf("save warbands.json: %v", err)
	}

	return townRoot
}

func TestRunCrewList_AllWithRigErrors(t *testing.T) {
	townRoot := setupTestTownForCrewList(t, map[string][]string{"warband-a": {"alice"}})

	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	crewListAll = true
	crewRig = "warband-a"
	defer func() {
		crewListAll = false
		crewRig = ""
	}()

	err := runCrewList(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected error for --all with --warband, got nil")
	}
}

func TestRunCrewList_AllAggregatesJSON(t *testing.T) {
	townRoot := setupTestTownForCrewList(t, map[string][]string{
		"warband-a": {"alice"},
		"warband-b": {"bob"},
	})

	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	if err := os.Chdir(townRoot); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	crewListAll = true
	crewJSON = true
	crewRig = ""
	defer func() {
		crewListAll = false
		crewJSON = false
	}()

	output := captureStdout(t, func() {
		if err := runCrewList(&cobra.Command{}, nil); err != nil {
			t.Fatalf("runCrewList failed: %v", err)
		}
	})

	var items []CrewListItem
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 clan workers, got %d", len(items))
	}

	warbands := map[string]bool{}
	for _, item := range items {
		warbands[item.Warband] = true
	}
	if !warbands["warband-a"] || !warbands["warband-b"] {
		t.Fatalf("expected clan from warband-a and warband-b, got: %#v", warbands)
	}
}
