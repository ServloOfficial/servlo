package cli

import (
	"fmt"

	nodeDet "github.com/realrashid/servlo/internal/node"
	"github.com/spf13/cobra"
)

// NewNodeUninstallCmd returns the node:uninstall command.
func NewNodeUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "node:uninstall <version>",
		Short: "Uninstall a Node.js version",
		Args:  cobra.ExactArgs(1),
		RunE:  runNodeUninstall,
	}
}

func runNodeUninstall(_ *cobra.Command, args []string) error {
	if !servloManagesNode() {
		return fmt.Errorf("servlo is not managing Node.js; nothing to uninstall")
	}
	if err := nodeDet.Active().Uninstall(args[0]); err != nil {
		return err
	}
	return nil
}
