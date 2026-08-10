package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/siteops"
	servloSystemd "github.com/realrashid/servlo/internal/systemd"
	"github.com/spf13/cobra"
)

// NewSecureCmd returns the secure command.
func NewSecureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secure [name]",
		Short: "Enable HTTPS for the current site",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSecure,
	}
	cmd.Flags().Bool("renew", false, "Reissue the cert on an already-secured site, resetting expiry")
	cmd.Flags().Bool("staging", false,
		"Issue from Let's Encrypt staging instead of production, for this and every later issuance (sets certs.staging)")
	return cmd
}

// NewUnsecureCmd returns the unsecure command.
func NewUnsecureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unsecure [name]",
		Short: "Disable HTTPS for the current site",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUnsecure,
	}
}

func resolveSiteName(args []string) (string, error) {
	if len(args) > 0 {
		return args[0], nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Look up by path first so directory names like "astrolov.com" resolve
	// correctly to their registered site name (e.g. "astrolov").
	if site, err := config.FindSiteByPath(cwd); err == nil {
		return site.Name, nil
	}
	return filepath.Base(cwd), nil
}

func runSecure(cmd *cobra.Command, args []string) error {
	if cmd != nil {
		// Applied before issuing, so the certificate this run produces comes
		// from the authority the flag names.
		if cmd.Flags().Changed("staging") {
			staging, _ := cmd.Flags().GetBool("staging")
			if err := applyStagingFlag(staging); err != nil {
				return err
			}
		}
		if renew, _ := cmd.Flags().GetBool("renew"); renew {
			return renewCert(args)
		}
	}
	return toggleSecureCmd(args, true)
}

// applyStagingFlag records the authority for the whole install rather than
// overriding a single issuance. Issuance and renewal read the same setting, so
// a staging certificate an operator issued to test their DNS is not quietly
// replaced by a production one at the 30-day mark, spending the production rate
// limit the staging run existed to protect.
func applyStagingFlag(staging bool) error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("reading the global config: %w", err)
	}
	if cfg.Certs.Staging == staging {
		return nil
	}
	cfg.Certs.Staging = staging
	if err := config.SaveGlobal(cfg); err != nil {
		return fmt.Errorf("recording the certificate authority: %w", err)
	}
	if staging {
		feedback.Note("issuing from Let's Encrypt staging: certificates will not be trusted by browsers, and every later issuance uses staging until you run this with --staging=false")
	} else {
		feedback.Note("issuing from Let's Encrypt production")
	}
	return nil
}

func runUnsecure(_ *cobra.Command, args []string) error {
	return toggleSecureCmd(args, false)
}

// renewCert force-reissues the site's certificate through siteops.RenewCert (the
// single source of truth) so a long-lived cert can be reset
// without toggling HTTPS off and on. Backs `servlo secure --renew`.
func renewCert(args []string) error {
	name, err := resolveSiteName(args)
	if err != nil {
		return err
	}
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found — run 'servlo link' first", name)
	}
	feedback.Begin()
	step := feedback.Start("renewing certificate")
	if err := siteops.RenewCert(site); err != nil {
		step.Fail(err)
		return err
	}
	step.OK(feedback.Val("https://" + site.PrimaryDomain()))
	return nil
}

// toggleSecureCmd is the CLI entry-point shared by `servlo secure` and
// `servlo unsecure`. It delegates the core flip to siteops.SetSecured (the
// single source of truth shared with the UI code paths) and supplies
// CLI-specific post-toggle hooks, today the Stripe listener restart.
func toggleSecureCmd(args []string, secured bool) error {
	name, err := resolveSiteName(args)
	if err != nil {
		return err
	}
	site, err := config.FindSite(name)
	if err != nil {
		return fmt.Errorf("site %q not found — run 'servlo link' first", name)
	}
	verb := "enabling HTTPS"
	if !secured {
		verb = "disabling HTTPS"
	}
	feedback.Begin()
	step := feedback.Start(verb)
	cascaded, err := siteops.SetSecuredCascade(site, secured)
	if err != nil {
		step.Fail(err)
		return err
	}
	scheme := "http"
	if secured {
		scheme = "https"
	}
	step.OK(feedback.Val(scheme + "://" + site.PrimaryDomain()))
	if len(cascaded) > 0 {
		feedback.Note("also secured group secondaries: " + strings.Join(cascaded, ", "))
	}
	return nil
}

// RestartStripeIfActive is exported so the daemon's stripe:refresh HTTP
// handler can run the same Stripe restart logic as the CLI. SetSecured
// posts to that endpoint after every toggle, so this is the single
// implementation across the CLI and the UI.
func RestartStripeIfActive(site *config.Site) { restartStripeIfActive(site) }

// restartStripeIfActive restarts the Stripe listener for the site if it is currently running,
// so that --forward-to picks up the new http/https scheme.
func restartStripeIfActive(site *config.Site) {
	unitName := "servlo-stripe-" + site.Name
	if !servloSystemd.IsServiceActive(unitName) {
		return
	}
	scheme := "http"
	if site.Secured {
		scheme = "https"
	}
	baseURL := scheme + "://" + site.PrimaryDomain()
	if err := StripeStartForSite(site.Name, site.Path, baseURL); err != nil {
		feedback.Warn("updating stripe listener unit: %v", err)
		return
	}
	if err := servloSystemd.RestartService(unitName); err != nil {
		feedback.Warn("restarting stripe listener: %v", err)
		return
	}
	fmt.Printf("  Restarted stripe listener → %s%s\n", baseURL, config.StripeWebhookPath(site.Path))
}
