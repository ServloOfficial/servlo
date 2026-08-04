package cli

import (
	"fmt"
	"os"

	"github.com/realrashid/servlo/internal/tui"
	"github.com/realrashid/servlo/internal/version"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// NewTuiCmd returns the `servlo tui` command. Opens a btop-style dashboard in
// the terminal with live site / service / worker status and keybindings for
// common start / stop / restart actions.
func NewTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Open a terminal dashboard for sites, services, and workers",
		RunE: func(_ *cobra.Command, _ []string) error {
			if !term.IsTerminal(int(os.Stdout.Fd())) {
				return fmt.Errorf("servlo tui requires an interactive terminal")
			}
			return tui.Run(version.Version)
		},
	}
}
