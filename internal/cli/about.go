package cli

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/version"
	"github.com/spf13/cobra"
)

// NewAboutCmd returns the about command.
func NewAboutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "about",
		Short: "Show information about Servlo",
		RunE:  runAbout,
	}
}

func runAbout(_ *cobra.Command, _ []string) error {
	feedback.Begin()
	fmt.Println("  " + feedback.Title("servlo"))
	fmt.Println("  " + feedback.Dim("Podman-powered PHP server panel for Ubuntu 24.04 LTS"))
	feedback.NewSummary().
		Row("Version", feedback.Val(version.Version)).
		Row("Commit", version.Commit).
		Row("Built", version.Date).
		Row("Repo", feedback.Val("https://github.com/ServloOfficial/servlo")).
		Print()
	feedback.Begin()
	fmt.Println("  " + feedback.Dim("© George Dumitrescu"))
	return nil
}
