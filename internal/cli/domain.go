package cli

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/feedback"
	"github.com/ServloOfficial/servlo/internal/grouping"
	"github.com/ServloOfficial/servlo/internal/linker"
	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/siteops"
	"github.com/spf13/cobra"
)

// NewDomainCmd returns the domain command with add/remove/list subcommands.
func NewDomainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Manage domains for the current site",
	}
	cmd.AddCommand(newDomainAddCmd())
	cmd.AddCommand(newDomainRemoveCmd())
	cmd.AddCommand(newDomainListCmd())
	return cmd
}

func newDomainAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name>",
		Short: "Add a domain to the current site (the full domain, e.g. example.com)",
		Args:  cobra.ExactArgs(1),
		RunE:  runDomainAdd,
	}
}

func newDomainRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a domain from the current site",
		Args:  cobra.ExactArgs(1),
		RunE:  runDomainRemove,
	}
}

func newDomainListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List domains for the current site",
		RunE:  runDomainList,
	}
}

// resolveSiteForCwd finds the site registered for the current working directory.
func resolveSiteForCwd() (*config.Site, error) {
	return ensureSiteForCwd()
}

func runDomainAdd(_ *cobra.Command, args []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	fullDomain, err := siteops.NormalizeDomain(args[0])
	if err != nil {
		return err
	}

	if linker.IsReservedDomain(fullDomain) {
		return fmt.Errorf("domain %q is reserved for internal Servlo use", fullDomain)
	}

	// Check if already present on this site.
	if site.HasDomain(fullDomain) {
		return fmt.Errorf("site %q already has domain %q", site.Name, fullDomain)
	}

	// Check if used by another site (strict — regardless of TLS scheme).
	if existing, err := config.IsDomainUsed(fullDomain); err == nil && existing != nil {
		return fmt.Errorf("domain %q is already used by site %q", fullDomain, existing.Name)
	}

	// Remove old vhost before updating domains (file is named after primary domain).
	oldPrimary := site.PrimaryDomain()

	site.Domains = append(site.Domains, fullDomain)

	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	// Sync to .servlo.yaml.
	_ = config.SyncProjectDomains(site.Path, site.Domains)

	// Regenerate vhost (file stays named after primary domain).
	if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
		return err
	}

	// If secured, force-reissue the cert so the SAN list picks up the new
	// domain.
	if site.Secured {
		if err := certs.ReissueCert(*site); err != nil {
			feedback.Warn("reissuing certificate: %v", err)
		}
	}

	if err := podman.WriteContainerHosts(); err != nil {
		feedback.Warn("updating container hosts file: %v", err)
	}

	nginx.ReloadOrWarn("")

	if err := siteops.SyncEnvIfPrimaryChanged(site, oldPrimary); err != nil {
		feedback.Warn("syncing .env to new primary domain: %v", err)
	}

	if site.IsGroupMain() {
		if err := grouping.CascadeMainDomainChange(site); err != nil {
			feedback.Warn("cascading group domain change: %v", err)
		}
	}

	feedback.Begin()
	feedback.Done("added " + feedback.Val(fullDomain) + " to " + site.Name)
	return nil
}

func runDomainRemove(_ *cobra.Command, args []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	fullDomain, err := siteops.NormalizeDomain(args[0])
	if err != nil {
		return err
	}

	if !site.HasDomain(fullDomain) {
		return fmt.Errorf("site %q does not have domain %q", site.Name, fullDomain)
	}

	if len(site.Domains) <= 1 {
		return fmt.Errorf("cannot remove the last domain from site %q", site.Name)
	}

	oldPrimary := site.PrimaryDomain()

	// Remove the domain.
	var newDomains []string
	for _, d := range site.Domains {
		if d != fullDomain {
			newDomains = append(newDomains, d)
		}
	}
	site.Domains = newDomains

	if err := config.AddSite(*site); err != nil {
		return fmt.Errorf("updating site registry: %w", err)
	}

	// Sync to .servlo.yaml, dropping the removed domain so it doesn't re-register.
	_ = config.ReplaceProjectDomain(site.Path, site.Domains, fullDomain)

	// If the primary domain changed (we removed the old primary), rename the vhost file.
	if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
		return err
	}

	// If secured, force-reissue the cert so the SAN list drops the removed
	// domain.
	if site.Secured {
		if err := certs.ReissueCert(*site); err != nil {
			feedback.Warn("reissuing certificate: %v", err)
		}
	}

	if err := podman.WriteContainerHosts(); err != nil {
		feedback.Warn("updating container hosts file: %v", err)
	}

	nginx.ReloadOrWarn("")

	if err := siteops.SyncEnvIfPrimaryChanged(site, oldPrimary); err != nil {
		feedback.Warn("syncing .env to new primary domain: %v", err)
	}

	if site.IsGroupMain() {
		if err := grouping.CascadeMainDomainChange(site); err != nil {
			feedback.Warn("cascading group domain change: %v", err)
		}
	}

	feedback.Begin()
	feedback.Done("removed " + feedback.Val(fullDomain) + " from " + site.Name)
	return nil
}

func runDomainList(_ *cobra.Command, _ []string) error {
	site, err := resolveSiteForCwd()
	if err != nil {
		return err
	}

	for i, d := range site.Domains {
		if i == 0 {
			fmt.Printf("  %s (primary)\n", d)
		} else {
			fmt.Printf("  %s\n", d)
		}
	}
	return nil
}
