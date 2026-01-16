// hd is the Horde CLI for managing multi-agent workspaces.
package main

import (
	"os"

	"github.com/OWNER/horde/internal/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
