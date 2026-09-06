package cli

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
	phpPkg "github.com/ServloOfficial/servlo/internal/php"
	"github.com/spf13/cobra"
)

// NewPhpListCmd returns the php:list command.
func NewPhpListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "php:list",
		Short: "List installed PHP versions",
		RunE:  runPhpList,
	}
}

func runPhpList(_ *cobra.Command, _ []string) error {
	versions, err := phpPkg.ListInstalled()
	if err != nil {
		return fmt.Errorf("listing PHP versions: %w", err)
	}

	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}

	if len(versions) == 0 {
		fmt.Println("No PHP versions installed in servlo bin directory.")
		fmt.Println("Run 'servlo install' to set up PHP, or 'servlo use <version>' to install a specific version.")
		return nil
	}

	fmt.Println("Installed PHP versions:")
	for _, v := range versions {
		marker := "  "
		if v == cfg.PHP.DefaultVersion {
			marker = "* "
		}
		fmt.Printf("%s%s\n", marker, v)
	}

	return nil
}
