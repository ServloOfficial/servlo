package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ServloOfficial/servlo/internal/auditlog"
	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dnscheck"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

// NewPanelCmd returns the `servlo panel` command group.
//
// The panel is reachable two ways and both are meant to be used. By address on
// port 7073 over a certificate it signed itself, which is what a fresh droplet
// has before any DNS exists; and on a domain, through nginx, with a real
// certificate from the same Get SSL flow every site uses. Attaching a domain is
// the second half of that, and it is one command because everything after it
// (the vhost, the challenge location, the certificate, the redirect) is the
// machinery sites already have.
func NewPanelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "panel",
		Short: "Show or change how the Servlo panel is reached",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return showPanel() },
	}
	cmd.AddCommand(newPanelDomainCmd())
	return cmd
}

func newPanelDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Show, set or remove the panel's domain",
		Args:  cobra.NoArgs,
		RunE:  func(*cobra.Command, []string) error { return showPanel() },
	}
	cmd.AddCommand(newPanelDomainSetCmd(), newPanelDomainSecureCmd(), newPanelDomainRemoveCmd())
	return cmd
}

func newPanelDomainSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <fqdn>",
		Short: "Serve the panel on a domain",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			domain := strings.ToLower(strings.TrimSpace(args[0]))
			if err := validatePanelDomain(domain); err != nil {
				return err
			}
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			cfg.UI.Domain = domain
			if err := config.SaveGlobal(cfg); err != nil {
				return err
			}
			recordAudit(auditlog.Entry{Action: "panel.domain.set", Subject: domain})

			feedback.Begin()
			write := feedback.Start("writing the panel vhost")
			if err := ApplyPanelDomain(); err != nil {
				write.Fail(err)
				return err
			}
			write.OK(feedback.Val(domain))

			// The DNS answer is the whole difference between "click Get SSL"
			// and "wait", so say which one it is rather than leaving the
			// operator to discover it from a failed issuance.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			report := dnscheck.Check(ctx, []string{domain})
			if report.Ready() {
				feedback.Note("DNS points here, so a certificate can be issued now: servlo panel domain secure")
			} else {
				feedback.Note(report.Summary())
			}
			feedback.Note("until then the panel keeps answering on port 7073 with its self-signed certificate")
			return nil
		},
	}
}

func newPanelDomainSecureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "secure",
		Short: "Issue a real certificate for the panel's domain",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			domain := strings.TrimSpace(cfg.UI.Domain)
			if domain == "" {
				return fmt.Errorf("the panel has no domain. Attach one first: servlo panel domain set panel.example.com")
			}

			feedback.Begin()
			issue := feedback.Start("issuing a certificate for " + domain)
			// The same issuer, the same DNS gate and the same directory as
			// every site, so a certificate for the panel is not a second thing
			// to keep renewing.
			certsDir, err := panelCertsDir()
			if err != nil {
				issue.Fail(err)
				return err
			}
			if err := certs.IssueCert(domain, []string{domain}, certsDir); err != nil {
				issue.Fail(err)
				return err
			}
			issue.OK(feedback.Val(domain))

			if err := ApplyPanelDomain(); err != nil {
				return err
			}
			recordAudit(auditlog.Entry{Action: "panel.domain.secured", Subject: domain})
			feedback.Done("the panel is on https://" + domain)
			feedback.Note("apply the vhost with: servlo restart")
			return nil
		},
	}
}

// panelCertsDir is the same directory sites use, so the renewal scanner that
// already walks it picks the panel up without being told about it.
func panelCertsDir() (string, error) {
	dir := filepath.Join(config.CertsDir(), "sites")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("creating the certificate directory: %w", err)
	}
	return dir, nil
}

func newPanelDomainRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Stop serving the panel on its domain",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.LoadGlobal()
			if err != nil {
				return err
			}
			if cfg.UI.Domain == "" {
				feedback.Begin()
				feedback.Note("the panel has no domain")
				return nil
			}
			previous := cfg.UI.Domain
			cfg.UI.Domain = ""
			if err := config.SaveGlobal(cfg); err != nil {
				return err
			}
			if err := nginx.RemovePanelVhost(); err != nil {
				return err
			}
			recordAudit(auditlog.Entry{Action: "panel.domain.removed", Subject: previous})

			feedback.Begin()
			feedback.Done("the panel no longer answers on " + feedback.Val(previous))
			// The certificate is left where it is. Removing a domain is often
			// a step in moving one, and re-issuing costs an ACME rate limit
			// slot that deleting a working certificate throws away.
			feedback.Note("reach it by address on port 7073; its certificate is kept in case the domain comes back")
			return nil
		},
	}
}

// ApplyPanelDomain writes or removes the panel's vhost so nginx matches the
// configured domain. Called after the domain changes and at start, so a config
// edited by hand takes effect rather than waiting for the next set.
func ApplyPanelDomain() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	domain := strings.TrimSpace(cfg.UI.Domain)
	if domain == "" {
		return nginx.RemovePanelVhost()
	}
	if err := nginx.EnsurePanelVhost(domain, certs.CertExists(domain)); err != nil {
		return err
	}
	return nil
}

// validatePanelDomain rejects what nginx would reject, and the shapes that
// would silently produce a vhost nobody can reach.
func validatePanelDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("a domain is required, e.g. servlo panel domain set panel.example.com")
	}
	if strings.ContainsAny(domain, " \t\n;{}\"'$/\\") {
		return fmt.Errorf("%q is not a domain", domain)
	}
	if strings.HasPrefix(domain, "*.") {
		return fmt.Errorf("the panel needs one name, not a wildcard")
	}
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("%q is not a fully qualified domain name", domain)
	}
	return nil
}

func showPanel() error {
	cfg, err := config.LoadGlobal()
	if err != nil {
		return err
	}
	feedback.Begin()
	if domain := strings.TrimSpace(cfg.UI.Domain); domain != "" {
		if certs.CertExists(domain) {
			feedback.Done("the panel is on https://" + domain)
		} else {
			feedback.Line("the panel is on http://" + domain + ", with no certificate yet")
			feedback.Note("issue one once DNS points here: servlo panel domain secure")
		}
	} else {
		feedback.Line("the panel has no domain")
		feedback.Note("attach one with: servlo panel domain set panel.example.com")
	}
	feedback.Note("it is always reachable by address on port 7073, over a self-signed certificate")
	return nil
}
