package cli

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/geodro/lerd/internal/config"
	"github.com/spf13/cobra"
)

// NewNodeInstallCmd returns the node:install command.
func NewNodeInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node:install <version>",
		Short: "Install a Node.js version via fnm",
		Args:  cobra.ExactArgs(1),
		RunE:  runNodeInstall,
	}
}

func runNodeInstall(_ *cobra.Command, args []string) error {
	if err := ensureNodeManaged(); err != nil {
		return err
	}
	version := args[0]
	fnmPath := filepath.Join(config.BinDir(), "fnm")
	cmd := exec.Command(fnmPath, "install", version)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Print(string(out))
	}
	if err != nil {
		return fmt.Errorf("fnm install %s: %w", version, err)
	}
	// Set as default if no default is configured yet.
	checkCmd := exec.Command(fnmPath, "exec", "--using=default", "--", "node", "--version")
	if checkCmd.Run() != nil {
		exec.Command(fnmPath, "default", version).Run() //nolint:errcheck
	}
	return nil
}
