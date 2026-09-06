package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/alerts"
	"github.com/spf13/cobra"
)

// NewAlertsCmd is `servlo alerts`: what is wrong with this server right now.
func NewAlertsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "What is currently wrong with this server",
		Long: "Sites that stopped answering, certificates that will not renew, backups that " +
			"failed or could not be restored, deploys that failed, workers that are down, and a " +
			"disk filling up.\n\n" +
			"An alert goes away on its own when the thing it is about recovers, so what is listed " +
			"here is what is wrong now rather than what has ever been wrong.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runAlerts() },
	}
	cmd.AddCommand(newAlertsClearCmd())
	return cmd
}

func runAlerts() error {
	open, err := alerts.List()
	if err != nil {
		return err
	}
	if len(open) == 0 {
		fmt.Println("Nothing is wrong.")
		return nil
	}
	for _, a := range open {
		where := a.Site
		if where == "" {
			where = "this server"
		}
		fmt.Printf("  %s  %s\n", a.At.Local().Format("2 Jan 15:04"), a.Subject())
		fmt.Printf("      on %s\n", where)
		for _, line := range strings.Split(strings.TrimSpace(a.Message), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				fmt.Printf("      %s\n", line)
			}
		}
		fmt.Println()
	}
	fmt.Printf("  %d %s. %s\n", len(open), plural(len(open), "alert", "alerts"), oldest(open))
	return nil
}

// oldest is the line that turns a list into a diagnosis. A failure that started
// three weeks ago is a different problem from one that started this morning,
// and the list alone does not say which.
func oldest(open []alerts.Alert) string {
	at := open[len(open)-1].At
	since := time.Since(at)
	switch {
	case since < time.Hour:
		return "The oldest started less than an hour ago."
	case since < 48*time.Hour:
		return fmt.Sprintf("The oldest started %d hours ago.", int(since.Hours()))
	default:
		return fmt.Sprintf("The oldest started %d days ago.", int(since.Hours()/24))
	}
}

func newAlertsClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear <kind> [site]",
		Short: "Take an alert off the list by hand",
		Long: "Alerts clear themselves when the thing they are about recovers, so this is for the " +
			"one that did not: a failure servlo cannot see the end of, or one you have decided to " +
			"live with.\n\n" +
			"It changes nothing about the server. If the failure is still happening, the next " +
			"check raises the alert again.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			var site string
			if len(args) > 1 {
				site = args[1]
			}
			if err := alerts.Clear(args[0], site); err != nil {
				return err
			}
			fmt.Println("  Cleared.")
			return nil
		},
	}
}
