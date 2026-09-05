package ui

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/grouping"
	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/siteops"
)

// The whole-system steps a domain change ends with, indirected so the handlers
// can be tested without an ACME server, a podman socket or a running nginx.
// Every one of them reaches outside this process, and a test that let them
// through would be testing the machine rather than the handler.
var (
	reissueCertFn = certs.ReissueCert
	writeHostsFn  = podman.WriteContainerHosts
	nginxReloadFn = nginx.Reload
)

// reissueAfterDomainChange brings a secured site's certificate back in line
// with the domains it now answers to, and returns what to tell the operator
// when it could not.
//
// The failure used to be dropped. Adding an alias is the case that made that
// untenable: the ordinary sequence is to add the domain and then point its DNS
// here, and the pre-flight refuses to issue for a domain that does not resolve
// here yet, so the reissue fails almost every time. The panel said OK, the site
// went on serving a certificate that did not name the new domain, and every
// visitor to it met a name-mismatch interstitial with nothing in the panel
// explaining why.
//
// The change still stands. Refusing it would make the usual order impossible,
// and a site answering on a domain with the wrong certificate is a state the
// operator asked for and can fix. What it must not be is silent (§3.3).
func reissueAfterDomainChange(site *config.Site) string {
	if !site.Secured {
		return ""
	}
	if err := reissueCertFn(*site); err != nil {
		return fmt.Sprintf(
			"The domains changed, but the certificate could not be reissued to cover %s: %v. "+
				"Point the DNS at this server, then use Get SSL.",
			strings.Join(site.Domains, ", "), err)
	}
	return ""
}

// handleSiteDomainAction serves the three domain mutations on
// /api/sites/{domain}/domain:{add,edit,remove}.
func handleSiteDomainAction(w http.ResponseWriter, r *http.Request, site *config.Site, action string) {
	switch action {
	case "domain:add":
		domainName := r.URL.Query().Get("name")
		if domainName == "" {
			writeJSON(w, SiteActionResponse{Error: "name parameter required"})
			return
		}
		fullDomain, dErr := siteops.NormalizeDomain(domainName)
		if dErr != nil {
			writeJSON(w, SiteActionResponse{Error: dErr.Error()})
			return
		}
		if site.HasDomain(fullDomain) {
			writeJSON(w, SiteActionResponse{Error: "site already has domain " + fullDomain})
			return
		}
		if existing, eErr := config.IsDomainUsed(fullDomain); eErr == nil && existing != nil {
			writeJSON(w, SiteActionResponse{Error: "domain " + fullDomain + " is already used by site " + existing.Name})
			return
		}
		oldPrimary := site.PrimaryDomain()
		site.Domains = append(site.Domains, fullDomain)
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.SyncProjectDomains(site.Path, site.Domains)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		warning := reissueAfterDomainChange(site)
		_ = writeHostsFn()
		_ = nginxReloadFn()
		if err := siteops.SyncEnvIfPrimaryChanged(site, oldPrimary); err != nil {
			fmt.Fprintf(os.Stderr, "servlo-panel: syncing .env to new primary domain: %v\n", err)
		}
		writeJSON(w, SiteActionResponse{OK: true, Warning: warning})
		return
	case "domain:edit":
		oldName := r.URL.Query().Get("old")
		newName := r.URL.Query().Get("new")
		if oldName == "" || newName == "" {
			writeJSON(w, SiteActionResponse{Error: "old and new parameters required"})
			return
		}
		oldDomain, oErr := siteops.NormalizeDomain(oldName)
		if oErr != nil {
			writeJSON(w, SiteActionResponse{Error: oErr.Error()})
			return
		}
		newDomain, nErr := siteops.NormalizeDomain(newName)
		if nErr != nil {
			writeJSON(w, SiteActionResponse{Error: nErr.Error()})
			return
		}
		if !site.HasDomain(oldDomain) {
			writeJSON(w, SiteActionResponse{Error: "site does not have domain " + oldDomain})
			return
		}
		if oldDomain != newDomain {
			if existing, eErr := config.IsDomainUsed(newDomain); eErr == nil && existing != nil && existing.Path != site.Path {
				writeJSON(w, SiteActionResponse{Error: "domain " + newDomain + " is already used by site " + existing.Name})
				return
			}
		}
		oldPrimary := site.PrimaryDomain()
		for i, d := range site.Domains {
			if d == oldDomain {
				site.Domains[i] = newDomain
				break
			}
		}
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.ReplaceProjectDomain(site.Path, site.Domains, oldDomain)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		warning := reissueAfterDomainChange(site)
		_ = writeHostsFn()
		_ = nginxReloadFn()
		if err := siteops.SyncEnvIfPrimaryChanged(site, oldPrimary); err != nil {
			fmt.Fprintf(os.Stderr, "servlo-panel: syncing .env to new primary domain: %v\n", err)
		}
		if site.IsGroupMain() {
			if err := grouping.CascadeMainDomainChange(site); err != nil {
				fmt.Fprintf(os.Stderr, "servlo-panel: cascading group domain change: %v\n", err)
			}
		}
		writeJSON(w, SiteActionResponse{OK: true, Warning: warning})
		return
	case "domain:remove":
		domainName := r.URL.Query().Get("name")
		if domainName == "" {
			writeJSON(w, SiteActionResponse{Error: "name parameter required"})
			return
		}
		fullDomain, dErr := siteops.NormalizeDomain(domainName)
		if dErr != nil {
			writeJSON(w, SiteActionResponse{Error: dErr.Error()})
			return
		}

		// If the domain isn't in the registered list, it might still be in the
		// project's .servlo.yaml as a conflict-filtered entry. Remove it from
		// .servlo.yaml only — no registry, vhost, or cert work needed.
		if !site.HasDomain(fullDomain) {
			declared := fullDomain
			// Check if domain exists in .servlo.yaml before removing.
			proj, projErr := config.LoadProjectConfig(site.Path)
			if projErr != nil || proj == nil {
				writeJSON(w, SiteActionResponse{Error: "site does not have domain " + fullDomain})
				return
			}
			found := false
			for _, d := range proj.Domains {
				if strings.EqualFold(d, declared) {
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, SiteActionResponse{Error: "site does not have domain " + fullDomain})
				return
			}
			if err := config.RemoveProjectDomain(site.Path, declared); err != nil {
				writeJSON(w, SiteActionResponse{Error: "updating .servlo.yaml: " + err.Error()})
				return
			}
			writeJSON(w, SiteActionResponse{OK: true})
			return
		}

		if len(site.Domains) <= 1 {
			writeJSON(w, SiteActionResponse{Error: "cannot remove the last domain"})
			return
		}
		oldPrimary := site.PrimaryDomain()
		var newDomains []string
		for _, d := range site.Domains {
			if d != fullDomain {
				newDomains = append(newDomains, d)
			}
		}
		site.Domains = newDomains
		if err := config.AddSite(*site); err != nil {
			writeJSON(w, SiteActionResponse{Error: "updating registry: " + err.Error()})
			return
		}
		_ = config.ReplaceProjectDomain(site.Path, site.Domains, fullDomain)
		if err := siteops.RegenerateSiteVhost(site, oldPrimary); err != nil {
			writeJSON(w, SiteActionResponse{Error: err.Error()})
			return
		}
		warning := reissueAfterDomainChange(site)
		_ = writeHostsFn()
		_ = nginxReloadFn()
		if err := siteops.SyncEnvIfPrimaryChanged(site, oldPrimary); err != nil {
			fmt.Fprintf(os.Stderr, "servlo-panel: syncing .env to new primary domain: %v\n", err)
		}
		if site.IsGroupMain() {
			if err := grouping.CascadeMainDomainChange(site); err != nil {
				fmt.Fprintf(os.Stderr, "servlo-panel: cascading group domain change: %v\n", err)
			}
		}
		writeJSON(w, SiteActionResponse{OK: true, Warning: warning})
		return
	}
	http.NotFound(w, r)
}
