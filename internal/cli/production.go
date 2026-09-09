package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/ServloOfficial/servlo/internal/auditlog"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/feedback"
)

// NewProductionCmd returns the production command group.
//
// The asymmetry between on and off is deliberate. Turning production mode on is
// confirmed because it changes what visitors see; turning it off needs --force
// because doing it on a live machine starts showing stack traces to the
// internet, and that is not something to do by autocompleting a command.
func NewProductionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "production",
		Short: "Show or change production mode",
		Long: `Production mode is one flag several other decisions read: whether PHP shows
errors to visitors, whether OPcache trusts its cache without checking the
filesystem, and how a worker restarts when it dies.

They belong together because they are the same question asked several ways.
Setting them individually is how a machine ends up half-production, showing
stack traces to the world while caching hard enough that nobody notices the fix.`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return showProductionMode() },
	}
	cmd.AddCommand(newProductionOnCmd(), newProductionOffCmd())
	return cmd
}

func showProductionMode() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	feedback.Begin()
	if cfg.ProductionMode() {
		feedback.Done("production mode is on since " + cfg.ProductionSince().Format("2006-01-02 15:04 UTC"))
		return nil
	}
	feedback.Note("production mode is off; errors are shown and OPcache re-checks the filesystem")
	return nil
}

func newProductionOnCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "on",
		Short: "Turn production mode on",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if cfg.ProductionMode() {
				feedback.Begin()
				feedback.Note("already on since " + cfg.ProductionSince().Format("2006-01-02 15:04 UTC"))
				return nil
			}
			if !yes && isInteractive() {
				feedback.Begin()
				feedback.Note("production mode hides PHP errors from visitors, stops OPcache re-reading changed files, and restarts workers on failure")
				if !feedback.Confirm("Turn production mode on?", true) {
					return nil
				}
			}
			return setProductionMode(cfg, true)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Do not ask for confirmation")
	return cmd
}

func newProductionOffCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "off",
		Short: "Turn production mode off (needs --force)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if !cfg.ProductionMode() {
				feedback.Begin()
				feedback.Note("production mode is already off")
				return nil
			}
			// A flag rather than a prompt, so it cannot be answered by muscle
			// memory or by a script piping "y". Turning this off on a live
			// machine starts showing stack traces to the internet.
			if !force {
				return fmt.Errorf("turning production mode off on a live machine starts showing PHP errors to visitors.\n\n" +
					"If that is what you want: servlo production off --force")
			}
			return setProductionMode(cfg, false)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Confirm turning production mode off")
	return cmd
}

func setProductionMode(cfg *config.GlobalConfig, on bool) error {
	cfg.SetProductionMode(on)
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	// Written here as well as on start, so the drop-in on disk matches the flag
	// the moment it changes rather than at the next restart.
	if err := config.WriteProductionIni(on); err != nil {
		return fmt.Errorf("writing the php production settings: %w", err)
	}
	action, message := "production.disabled", "production mode off"
	if on {
		action, message = "production.enabled", "production mode on"
	}
	recordAudit(auditlog.Entry{Action: action})

	feedback.Begin()
	feedback.Done(message)
	// `servlo restart` is one site's container, and typed outside a site
	// directory it resolves to the directory name and reports it is not a site.
	// Both halves of the mode need the whole stack: the PHP drop-in is re-read
	// when a site's containers come back, and the worker restart policy is
	// rewritten by start.
	feedback.Note("apply it to the running stack with: servlo stop, then servlo start")
	return nil
}
