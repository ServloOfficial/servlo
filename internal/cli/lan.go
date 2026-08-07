package cli

import (
	"fmt"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/spf13/cobra"
)

// NewLANCmd returns the `servlo lan` parent command. Exposure covers nginx and
// nothing else: databases, caches and admin UIs are loopback-only always, so
// there is one setting here rather than two.
func NewLANCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lan",
		Short: "Expose servlo to other devices on the local network",
		Long: `Control whether servlo sites are reachable from other devices on the
network.

By default every container PublishPort and the dashboard bind to loopback.
Run 'servlo lan:expose' to expose sites, DNS, and the dashboard listener.
Databases, caches and other managed services never leave loopback, whatever
this is set to.`,
	}
	cmd.AddCommand(newLANExposeCmd())
	cmd.AddCommand(newLANUnexposeCmd())
	cmd.AddCommand(newLANStatusCmd())
	return cmd
}

// NewLANExposeCmd returns the `servlo lan:expose` colon-style alias.
func NewLANExposeCmd() *cobra.Command {
	cmd := newLANExposeCmd()
	cmd.Use = "lan:expose"
	cmd.Hidden = true
	return cmd
}

// NewLANUnexposeCmd returns the `servlo lan:unexpose` colon-style alias.
func NewLANUnexposeCmd() *cobra.Command {
	cmd := newLANUnexposeCmd()
	cmd.Use = "lan:unexpose"
	cmd.Hidden = true
	return cmd
}

// NewLANStatusCmd returns the `servlo lan:status` colon-style alias.
func NewLANStatusCmd() *cobra.Command {
	cmd := newLANStatusCmd()
	cmd.Use = "lan:status"
	cmd.Hidden = true
	return cmd
}

func newLANExposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "expose",
		Short: "Make servlo reachable from other devices on the local network",
		Long: `Exposes servlo sites on a trusted local network:

  - Rewrites servlo-nginx so ports 80 and 443 bind to the LAN.
  - Restarts nginx when its bind changes.
  - Starts the userspace DNS forwarder where the platform requires it.

Databases, caches, and other managed services stay on loopback and are not
affected by this command.

The dashboard at port 7073 is gated by remote-control middleware. LAN clients
get 403 unless 'servlo remote-control on' has configured HTTP Basic auth.

The state persists in ~/.config/servlo/config.yaml. Re-running this command heals
drift between the config and installed runtime units.

Only use LAN exposure on a trusted network. Configure the host firewall for the
ports and devices that require access.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, _ := config.LoadGlobal()
			dnsOn := cfg == nil || cfg.DNS.Enabled
			// In disabled-DNS mode the dashboard is the only thing the LAN
			// exposure unlocks for remote devices, so bundle the
			// remote-control credential prompt into this single command if
			// it has not already been set.
			if !dnsOn && cfg != nil && cfg.UI.PasswordHash == "" {
				feedback.Line("DNS is disabled — the dashboard is the only thing reachable on the LAN, set HTTP Basic credentials so it is gated")
				if err := promptAndPersistRemoteControl(); err != nil {
					return err
				}
				cfg, _ = config.LoadGlobal()
			}
			feedback.Begin()
			expose := feedback.Start("exposing servlo on the LAN")
			lanIP, err := EnableLANExposure(func(step string) {
				feedback.Note(step)
			})
			if err != nil {
				expose.Fail(err)
				return err
			}
			expose.OK(feedback.Val(lanIP))
			if dnsOn {
				feedback.Note("sites are reachable on " + lanIP)
			} else {
				feedback.Note("sites: reachable on the host's LAN address once exposed")
			}
			if cfg != nil && cfg.UI.PasswordHash != "" {
				feedback.Note(fmt.Sprintf("dashboard: https://%s:7073 (HTTP Basic auth required)", lanIP))
			} else {
				feedback.Note(fmt.Sprintf("dashboard: https://%s:7073 (LAN clients get 403 — run `servlo remote-control on` to grant LAN access)", lanIP))
			}
			feedback.Note("managed services: loopback-only, always")
			if dnsOn {
				feedback.Note("allow ports 80, 443, 5300, 7073 through your firewall; `servlo remote-setup` generates a one-time bootstrap code")
			} else {
				feedback.Note("allow ports 80, 443 and 7073 through your firewall")
			}
			return nil
		},
	}
}

func newLANUnexposeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unexpose",
		Short: "Restrict servlo to loopback only — safe for untrusted wifi",
		RunE: func(_ *cobra.Command, _ []string) error {
			feedback.Begin()
			restrict := feedback.Start("restricting servlo to loopback")
			if err := DisableLANExposure(func(step string) {
				feedback.Note(step)
			}); err != nil {
				restrict.Fail(err)
				return err
			}
			restrict.OK(feedback.Val("127.0.0.1"))
			feedback.Note("LAN devices can no longer reach sites, services, or the dashboard")
			feedback.Note("any active remote-setup code has been revoked")
			return nil
		},
	}
}

func newLANStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether servlo is currently exposed to the local network",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			feedback.Begin()
			if cfg.LAN.Exposed {
				lanIP, _ := detectPrimaryLANIP()
				if lanIP == "" {
					lanIP = "(unknown)"
				}
				feedback.Done("exposed to the LAN at " + feedback.Val(lanIP))
				feedback.Note("managed services: loopback-only, always")
			} else {
				feedback.Line("loopback-only (127.0.0.1) — LAN devices cannot reach it")
			}
			return nil
		},
	}
}
