package cmd

import (
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/OWNER/horde/internal/web"
	"github.com/OWNER/horde/internal/workspace"
)

var (
	dashboardPort int
	dashboardOpen bool
)

var dashboardCmd = &cobra.Command{
	Use:     "warmap",
	GroupID: GroupDiag,
	Short:   "Start the raid tracking web warmap",
	Long: `Start a web server that displays the raid tracking warmap.

The warmap shows real-time raid status with:
- Raid list with status indicators
- Progress tracking for each raid
- Last activity indicator (green/yellow/red)
- Auto-refresh every 30 seconds via htmx

Example:
  hd warmap              # Start on default port 8080
  hd warmap --port 3000  # Start on port 3000
  hd warmap --open       # Start and open browser`,
	RunE: runDashboard,
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8080, "HTTP port to listen on")
	dashboardCmd.Flags().BoolVar(&dashboardOpen, "open", false, "Open browser automatically")
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	// Verify we're in a workspace
	if _, err := workspace.FindFromCwdOrError(); err != nil {
		return fmt.Errorf("not in a Horde workspace: %w", err)
	}

	// Create the live raid fetcher
	fetcher, err := web.NewLiveRaidFetcher()
	if err != nil {
		return fmt.Errorf("creating raid fetcher: %w", err)
	}

	// Create the handler
	handler, err := web.NewRaidHandler(fetcher)
	if err != nil {
		return fmt.Errorf("creating raid handler: %w", err)
	}

	// Build the URL
	url := fmt.Sprintf("http://localhost:%d", dashboardPort)

	// Open browser if requested
	if dashboardOpen {
		go openBrowser(url)
	}

	// Start the server with timeouts
	fmt.Printf("🚚 Horde Warmap starting at %s\n", url)
	fmt.Printf("   Press Ctrl+C to stop\n")

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", dashboardPort),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return server.ListenAndServe()
}

// openBrowser opens the specified URL in the default browser.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return
	}
	_ = cmd.Start()
}
