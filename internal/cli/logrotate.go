package cli

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/logrotate"
	"github.com/spf13/cobra"
)

// Rotation hangs off `servlo logs`, beside the container-log viewer, because
// "the logs" is one subject to an operator even though the two halves live in
// completely different places: one is podman's output, the other is files
// inside a site.

// newLogsRetentionCmds are the rotation half of `servlo logs`.
func newLogsRetentionCmds() []*cobra.Command {
	return []*cobra.Command{newLogsRotateCmd(), newLogsKeepCmd()}
}

func runLogsShow() error {
	p := logrotate.SitePolicy()
	if !logrotate.Enabled() {
		fmt.Println("  Rotation is off. Nothing servlo runs rotates a site's logs.")
		fmt.Println("  Turn it back on with: servlo logs keep --on")
		return nil
	}
	compressed := "compressed"
	if !p.Compress {
		compressed = "uncompressed"
	}
	fmt.Printf("  Rotating at %d MB, keeping %d %s.\n", p.MaxSizeMB, p.Keep, compressed)
	fmt.Printf("  Daily at %s, and on demand with: servlo logs rotate\n\n", logrotate.Calendar)

	fmt.Println("  nginx, PHP-FPM and the workers log to the journal, not to files servlo owns.")
	fmt.Println("  To bound it, which needs root:")
	for _, c := range logrotate.JournalCommands("1G") {
		fmt.Printf("    %s\n", c)
	}
	return nil
}

func newLogsRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate",
		Short: "Rotate every site's logs now",
		Long: "What the daily timer runs. A log under the size limit is left alone, so running " +
			"this by hand does not split a log an operator is halfway through reading.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error { return runLogsRotate() },
	}
}

func runLogsRotate() error {
	results, err := logrotate.RotateAll()
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("  Nothing was big enough to rotate.")
		return nil
	}
	var failed int
	for _, r := range results {
		if r.Err != nil {
			failed++
			fmt.Printf("  %s: %v\n", r.Site, r.Err)
			continue
		}
		what := fmt.Sprintf("%d %s rotated", len(r.Rotated), plural(len(r.Rotated), "log", "logs"))
		if r.Removed > 0 {
			what += fmt.Sprintf(", %d old %s removed", r.Removed, plural(r.Removed, "copy", "copies"))
		}
		fmt.Printf("  %s: %s, %s of live log freed\n", r.Site, what, humanSize(r.Freed))
	}
	if failed > 0 {
		// Not an error exit. The sites that rotated did rotate, and failing the
		// command would have a timer report the whole run as failed.
		fmt.Printf("\n  %d %s could not be rotated.\n", failed, plural(failed, "site", "sites"))
	}
	return nil
}

func newLogsKeepCmd() *cobra.Command {
	var maxSize, keep int
	var on, off, uncompressed, compressed bool
	cmd := &cobra.Command{
		Use:   "keep",
		Short: "How much log to keep",
		Long: "Rotation happens when a log passes --max-size, and --keep copies survive. " +
			"Changing either applies from the next rotation; nothing already on disk is touched.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if on && off {
				return fmt.Errorf("--on and --off cannot both be given")
			}
			if uncompressed && compressed {
				return fmt.Errorf("--compress and --no-compress cannot both be given")
			}
			return runLogsKeep(c, maxSize, keep, on, off, uncompressed, compressed)
		},
	}
	cmd.Flags().IntVar(&maxSize, "max-size", 0, "Rotate a log once it passes this many megabytes")
	cmd.Flags().IntVar(&keep, "keep", 0, "How many rotated copies to keep")
	cmd.Flags().BoolVar(&on, "on", false, "Rotate logs on a daily timer")
	cmd.Flags().BoolVar(&off, "off", false, "Stop rotating, and remove the timer")
	cmd.Flags().BoolVar(&compressed, "compress", false, "Gzip each rotated copy (the default)")
	cmd.Flags().BoolVar(&uncompressed, "no-compress", false, "Leave rotated copies as plain text")
	return cmd
}

func runLogsKeep(cmd *cobra.Command, maxSize, keep int, on, off, uncompressed, compressed bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	if maxSize > 0 {
		cfg.Logs.MaxSizeMB = maxSize
	}
	if keep > 0 {
		cfg.Logs.Keep = keep
	}
	if uncompressed {
		cfg.Logs.KeepUncompressed = true
	}
	if compressed {
		cfg.Logs.KeepUncompressed = false
	}
	if on {
		cfg.Logs.Disabled = false
	}
	if off {
		cfg.Logs.Disabled = true
	}
	if !cmd.Flags().Changed("max-size") && !cmd.Flags().Changed("keep") && !on && !off &&
		!uncompressed && !compressed {
		return runLogsShow()
	}
	if err := config.SaveGlobal(cfg); err != nil {
		return err
	}
	// The timer follows the setting straight away. A setting saved without the
	// units rewritten is a server whose configuration and behaviour disagree
	// until the next restart, which is the kind of gap nobody looks for.
	if err := logrotate.ApplySchedule(!cfg.Logs.Disabled, servloBinaryPath()); err != nil {
		fmt.Printf("  Saved, but the timer could not be updated: %v\n", err)
		return nil
	}
	return runLogsShow()
}
