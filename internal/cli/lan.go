package cli

import (
	"fmt"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/spf13/cobra"
)

// NewLANCmd returns the `servlo lan` parent command. Site exposure and managed
// service exposure are separate persisted settings: sites follow
// cfg.LAN.Exposed, while databases, caches, and other managed services require
// both cfg.LAN.Exposed and cfg.LAN.ServicesExposed.
func NewLANCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lan",
		Short: "Expose servlo to other devices on the local network",
		Long: `Control whether servlo sites and managed services are reachable from
other devices on the local network.

By default every container PublishPort and the dashboard bind to loopback.
Run 'servlo lan:expose' to expose sites, DNS, and the dashboard listener on a
trusted LAN. Managed databases, caches, and other services remain loopback-only
unless you explicitly run 'servlo lan:services on'.`,
	}
	cmd.AddCommand(newLANExposeCmd())
	cmd.AddCommand(newLANUnexposeCmd())
	cmd.AddCommand(newLANStatusCmd())
	cmd.AddCommand(newLANServicesCmd())
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

// NewLANServicesCmd returns the `servlo lan:services` colon-style command.
func NewLANServicesCmd() *cobra.Command {
	cmd := newLANServicesCmd()
	cmd.Use = "lan:services [on|off|status]"
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

Managed databases, caches, and other services stay loopback-only by default.
Run 'servlo lan:services on' once to include them. That preference persists and
applies automatically as services start, stop, or change ports.

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
				feedback.Note(fmt.Sprintf("dashboard: http://%s:7073 (HTTP Basic auth required)", lanIP))
			} else {
				feedback.Note(fmt.Sprintf("dashboard: http://%s:7073 (LAN clients get 403 — run `servlo remote-control on` to grant LAN access)", lanIP))
			}
			if cfg != nil && cfg.LAN.ServicesExposed {
				feedback.Note("managed services: exposed on their configured host ports")
			} else {
				feedback.Note("managed services: loopback-only (run `servlo lan:services on` to expose them)")
			}
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
func newLANServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "services [on|off|status]",
		Short:     "Control LAN access to managed databases, caches, and services",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"on", "off", "status"},
		RunE: func(_ *cobra.Command, args []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}

			if args[0] == "status" {
				feedback.Begin()
				switch {
				case cfg.LAN.ServicesExposed && cfg.LAN.Exposed:
					feedback.Done("managed service LAN access is active")
				case cfg.LAN.ServicesExposed:
					feedback.Line("managed service LAN access is enabled but inactive until `servlo lan:expose`")
				default:
					feedback.Line("managed service LAN access is off; services are loopback-only")
				}
				return nil
			}

			enabled := args[0] == "on"
			// Turning it on while servlo is loopback-only would persist a
			// setting that publishes nothing, so refuse rather than store an
			// inert preference. Turning it off always works, so a setting
			// armed before an unexpose can still be cleared.
			if enabled && !cfg.LAN.Exposed {
				return fmt.Errorf("LAN exposure is off — run `servlo lan:expose` first. Managed services can only reach the LAN while servlo itself does")
			}
			feedback.Begin()
			update := feedback.Start("updating managed service LAN access")
			if err := SetManagedServiceLANExposure(enabled, nil); err != nil {
				update.Fail(err)
				return err
			}
			if enabled {
				update.OK(feedback.Val("enabled"))
				feedback.Note("development services may use weak or empty credentials; restrict their ports with the host firewall and use only on a trusted network")
			} else {
				update.OK(feedback.Val("loopback-only"))
			}
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
				if cfg.LAN.ServicesExposed {
					feedback.Note("managed services: exposed")
				} else {
					feedback.Note("managed services: loopback-only")
				}
			} else {
				feedback.Line("loopback-only (127.0.0.1) — LAN devices cannot reach it")
			}
			return nil
		},
	}
}
