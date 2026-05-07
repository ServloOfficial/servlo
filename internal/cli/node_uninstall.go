package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewNodeUninstallCmd returns the node:uninstall command.
func NewNodeUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node:uninstall <version>",
		Short: "Uninstall a Node.js version via fnm",
		Args:  cobra.ExactArgs(1),
		RunE:  runNodeUninstall,
	}
}

func runNodeUninstall(_ *cobra.Command, args []string) error {
	if !lerdManagesNode() {
		return fmt.Errorf("lerd is not managing Node.js; nothing to uninstall")
	}
	version := args[0]
	fnmPath := filepath.Join(config.BinDir(), "fnm")
	cmd := exec.Command(fnmPath, "uninstall", version)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Print(string(out))
	}
	if err != nil {
		return fmt.Errorf("fnm uninstall %s: %w", version, err)
	}
	return nil
}
