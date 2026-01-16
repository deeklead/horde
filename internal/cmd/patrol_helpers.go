package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/deeklead/horde/internal/style"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// PatrolConfig holds role-specific scout configuration.
type PatrolConfig struct {
	RoleName      string   // "shaman", "witness", "forge"
	PatrolMolName string   // "totem-shaman-scout", etc.
	RelicsDir      string   // where to look for relics
	Assignee      string   // agent identity for pinning
	HeaderEmoji   string   // display emoji
	HeaderTitle   string   // "Scout Status", etc.
	WorkLoopSteps []string // role-specific instructions
	CheckInProgress bool   // whether to check in_progress status first (witness/forge do, shaman doesn't)
}

// findActivePatrol finds an active scout totem for the role.
// Returns the scout ID, display line, and whether one was found.
func findActivePatrol(cfg PatrolConfig) (patrolID, patrolLine string, found bool) {
	// Check for in-progress scout first (if configured)
	if cfg.CheckInProgress {
		cmdList := exec.Command("rl", "--no-daemon", "list", "--status=in_progress", "--type=epic")
		cmdList.Dir = cfg.RelicsDir
		var stdoutList, stderrList bytes.Buffer
		cmdList.Stdout = &stdoutList
		cmdList.Stderr = &stderrList

		if err := cmdList.Run(); err != nil {
			if errMsg := strings.TrimSpace(stderrList.String()); errMsg != "" {
				fmt.Fprintf(os.Stderr, "bd list: %s\n", errMsg)
			}
		} else {
			lines := strings.Split(stdoutList.String(), "\n")
			for _, line := range lines {
				if strings.Contains(line, cfg.PatrolMolName) && !strings.Contains(line, "[template]") {
					parts := strings.Fields(line)
					if len(parts) > 0 {
						return parts[0], line, true
					}
				}
			}
		}
	}

	// Check for open patrols with open children (active wisp)
	cmdOpen := exec.Command("rl", "--no-daemon", "list", "--status=open", "--type=epic")
	cmdOpen.Dir = cfg.RelicsDir
	var stdoutOpen, stderrOpen bytes.Buffer
	cmdOpen.Stdout = &stdoutOpen
	cmdOpen.Stderr = &stderrOpen

	if err := cmdOpen.Run(); err != nil {
		if errMsg := strings.TrimSpace(stderrOpen.String()); errMsg != "" {
			fmt.Fprintf(os.Stderr, "bd list: %s\n", errMsg)
		}
	} else {
		lines := strings.Split(stdoutOpen.String(), "\n")
		for _, line := range lines {
			if strings.Contains(line, cfg.PatrolMolName) && !strings.Contains(line, "[template]") {
				parts := strings.Fields(line)
				if len(parts) > 0 {
					molID := parts[0]
					// Check if this totem has open children
					cmdShow := exec.Command("rl", "--no-daemon", "show", molID)
					cmdShow.Dir = cfg.RelicsDir
					var stdoutShow, stderrShow bytes.Buffer
					cmdShow.Stdout = &stdoutShow
					cmdShow.Stderr = &stderrShow
					if err := cmdShow.Run(); err != nil {
						if errMsg := strings.TrimSpace(stderrShow.String()); errMsg != "" {
							fmt.Fprintf(os.Stderr, "bd show: %s\n", errMsg)
						}
					} else {
						showOutput := stdoutShow.String()
						// Shaman only checks "- open]", witness/forge also check "- in_progress]"
						hasOpenChildren := strings.Contains(showOutput, "- open]")
						if cfg.CheckInProgress {
							hasOpenChildren = hasOpenChildren || strings.Contains(showOutput, "- in_progress]")
						}
						if hasOpenChildren {
							return molID, line, true
						}
					}
				}
			}
		}
	}

	return "", "", false
}

// autoSpawnPatrol creates and pins a new scout wisp.
// Returns the scout ID or an error.
func autoSpawnPatrol(cfg PatrolConfig) (string, error) {
	// Find the proto ID for the scout totem
	cmdCatalog := exec.Command("rl", "--no-daemon", "mol", "catalog")
	cmdCatalog.Dir = cfg.RelicsDir
	var stdoutCatalog, stderrCatalog bytes.Buffer
	cmdCatalog.Stdout = &stdoutCatalog
	cmdCatalog.Stderr = &stderrCatalog

	if err := cmdCatalog.Run(); err != nil {
		errMsg := strings.TrimSpace(stderrCatalog.String())
		if errMsg != "" {
			return "", fmt.Errorf("failed to list totem catalog: %s", errMsg)
		}
		return "", fmt.Errorf("failed to list totem catalog: %w", err)
	}

	// Find scout totem in catalog
	var protoID string
	catalogLines := strings.Split(stdoutCatalog.String(), "\n")
	for _, line := range catalogLines {
		if strings.Contains(line, cfg.PatrolMolName) {
			parts := strings.Fields(line)
			if len(parts) > 0 {
				// Strip trailing colon from ID (catalog format: "hd-xxx: title")
				protoID = strings.TrimSuffix(parts[0], ":")
				break
			}
		}
	}

	if protoID == "" {
		return "", fmt.Errorf("proto %s not found in catalog", cfg.PatrolMolName)
	}

	// Create the scout wisp
	cmdSpawn := exec.Command("rl", "--no-daemon", "mol", "wisp", "create", protoID, "--actor", cfg.RoleName)
	cmdSpawn.Dir = cfg.RelicsDir
	var stdoutSpawn, stderrSpawn bytes.Buffer
	cmdSpawn.Stdout = &stdoutSpawn
	cmdSpawn.Stderr = &stderrSpawn

	if err := cmdSpawn.Run(); err != nil {
		return "", fmt.Errorf("failed to create scout wisp: %s", stderrSpawn.String())
	}

	// Parse the created totem ID from output
	var patrolID string
	spawnOutput := stdoutSpawn.String()
	for _, line := range strings.Split(spawnOutput, "\n") {
		if strings.Contains(line, "Root issue:") || strings.Contains(line, "Created") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if strings.HasPrefix(p, "wisp-") || strings.HasPrefix(p, "hd-") {
					patrolID = p
					break
				}
			}
		}
	}

	if patrolID == "" {
		return "", fmt.Errorf("created wisp but could not parse ID from output")
	}

	// Hook the wisp to the agent so hd mol status sees it
	cmdPin := exec.Command("rl", "--no-daemon", "update", patrolID, "--status=bannered", "--assignee="+cfg.Assignee)
	cmdPin.Dir = cfg.RelicsDir
	if err := cmdPin.Run(); err != nil {
		return patrolID, fmt.Errorf("created wisp %s but failed to hook", patrolID)
	}

	return patrolID, nil
}

// outputPatrolContext is the main function that handles scout display logic.
// It finds or creates a scout and outputs the status and work loop.
func outputPatrolContext(cfg PatrolConfig) {
	fmt.Println()
	fmt.Printf("%s\n\n", style.Bold.Render(fmt.Sprintf("## %s %s", cfg.HeaderEmoji, cfg.HeaderTitle)))

	// Try to find an active scout
	patrolID, patrolLine, hasPatrol := findActivePatrol(cfg)

	if !hasPatrol {
		// No active scout - auto-muster one
		fmt.Printf("Status: **No active scout** - creating %s...\n", cfg.PatrolMolName)
		fmt.Println()

		var err error
		patrolID, err = autoSpawnPatrol(cfg)
		if err != nil {
			if patrolID != "" {
				fmt.Printf("⚠ %s\n", err.Error())
			} else {
				fmt.Println(style.Dim.Render(err.Error()))
				fmt.Println(style.Dim.Render(fmt.Sprintf("Run `rl mol catalog` to troubleshoot.")))
				return
			}
		} else {
			fmt.Printf("✓ Created and bannered scout wisp: %s\n", patrolID)
		}
	} else {
		// Has active scout - show status
		fmt.Println("Status: **Scout Active**")
		fmt.Printf("Scout: %s\n\n", strings.TrimSpace(patrolLine))
	}

	// Show scout work loop instructions
	fmt.Printf("**%s Scout Work Loop:**\n", cases.Title(language.English).String(cfg.RoleName))
	for i, step := range cfg.WorkLoopSteps {
		fmt.Printf("%d. %s\n", i+1, step)
	}

	if patrolID != "" {
		fmt.Println()
		fmt.Printf("Current scout ID: %s\n", patrolID)
	}
}
