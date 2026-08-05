package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/certs"
	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/envfile"
	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/siteops"
)

// sitesWithTLD returns the names of registered sites that have at least one
// domain ending in "."+oldTLD, in registry order. Used to drive the install
// migration prompt when the user flips dns.enabled or otherwise picks a new
// TLD: only sites whose stored domains still carry the previous TLD are
// candidates for rewrite.
func sitesWithTLD(oldTLD string) []string {
	suffix := "." + oldTLD
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}
	var names []string
	for _, s := range reg.Sites {
		for _, d := range s.Domains {
			if strings.HasSuffix(d, suffix) {
				names = append(names, s.Name)
				break
			}
		}
	}
	return names
}

// projectWantsHTTPS reports whether the site's committed .servlo.yaml records
// HTTPS intent. It is the record the DNS re-enable migration restores from; a
// missing or unreadable file means no intent, so the site stays plain HTTP.
func projectWantsHTTPS(dir string) bool {
	cfg, err := config.LoadProjectConfig(dir)
	if err != nil || cfg == nil {
		return false
	}
	return cfg.Secured
}

// migrateSiteTLD rewrites every site's domain suffix from oldTLD to newTLD,
// removes stale nginx vhost confs at the previous primary-domain paths, and
// updates each site's .env APP_URL (plus Vite/Reverb keys) via
// envfile.SyncPrimaryDomain. When forceUnsecure is true (DNS being disabled,
// so HTTPS is unavailable) the site's registry Secured flag is flipped off so
// the regen pass writes plain HTTP vhosts, but the project's committed HTTPS
// intent in .servlo.yaml is left intact. When forceUnsecure is false (DNS being
// enabled) each site's Secured flag is restored from that intent, so a
// disable/enable round trip returns previously secured sites to https.
//
// Returns the list of sites that were actually mutated. Errors on individual
// sites are printed but do not stop the migration: a partial rename is still
// preferable to leaving the user halfway between two TLDs.
func migrateSiteTLD(oldTLD, newTLD string, forceUnsecure bool) []string {
	if oldTLD == "" || newTLD == "" || oldTLD == newTLD {
		return nil
	}
	oldSuffix := "." + oldTLD
	newSuffix := "." + newTLD

	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return nil
	}

	var changed []string
	for _, s := range reg.Sites {
		oldPrimary := s.PrimaryDomain()
		rewrote := false
		newDomains := make([]string, len(s.Domains))
		for i, d := range s.Domains {
			if strings.HasSuffix(d, oldSuffix) {
				newDomains[i] = strings.TrimSuffix(d, oldSuffix) + newSuffix
				rewrote = true
			} else {
				newDomains[i] = d
			}
		}
		if !rewrote {
			continue
		}

		s.Domains = newDomains
		// Registry flag off while DNS is down; on re-enable restore HTTPS from
		// the committed .servlo.yaml intent, or the registry-recorded pre-disable
		// state for a site with no .servlo.yaml, so the round trip is lossless.
		if forceUnsecure {
			s.SecuredBeforeDNSOff = s.Secured
			s.Secured = false
		} else {
			s.Secured = projectWantsHTTPS(s.Path) || s.SecuredBeforeDNSOff
			s.SecuredBeforeDNSOff = false
		}
		if err := config.AddSite(s); err != nil {
			fmt.Printf("    WARN: %s: persist domains: %v\n", s.Name, err)
			continue
		}

		removeStaleVhosts(oldPrimary)
		newPrimary := s.PrimaryDomain()
		// The hand-authored override is keyed by primary domain, so a TLD
		// rewrite would orphan it just like a UI domain rename does.
		if oldPrimary != newPrimary {
			if err := siteops.MoveCustomNginxConfig(oldPrimary, newPrimary); err != nil {
				fmt.Printf("    WARN: %s: migrate custom nginx override: %v\n", s.Name, err)
			}
		}
		// Reissue the cert under the NEW primary. Without this, SSL
		// handshakes to <newPrimary> fail because the old cert's SANs
		// still reference the old TLD. Skip when forceUnsecure flips
		// the site to plain HTTP — old certs go through removeStaleCerts.
		if s.Secured {
			if err := certs.ReissueCert(s); err != nil {
				fmt.Printf("    WARN: %s: reissue cert: %v\n", s.Name, err)
			}
		}
		// Clean up the old cert files at the previous primary so the
		// certs dir doesn't accumulate stale entries. Mirrors the
		// nginx-vhost removeStaleVhosts path above.
		if forceUnsecure || oldPrimary != newPrimary {
			removeStaleCerts(oldPrimary)
		}

		scheme := "http"
		if s.Secured {
			scheme = "https"
		}
		if err := envfile.SyncPrimaryDomain(s.Path, newPrimary, s.Secured); err != nil {
			fmt.Printf("    WARN: %s: update .env: %v\n", s.Name, err)
		}
		_ = config.SyncProjectDomains(s.Path, s.Domains, newTLD)

		feedback.Note(fmt.Sprintf("%s: %s → %s://%s", s.Name, oldPrimary, scheme, newPrimary))
		changed = append(changed, s.Name)
	}
	return changed
}

// adjustSitesSecuredForDNS tracks DNS availability for sites on a preserved
// (custom) TLD without renaming: disabling drops to http (certs follow),
// enabling restores HTTPS from .servlo.yaml or the recorded state.
func adjustSitesSecuredForDNS(tld string, enabling bool) {
	reg, err := config.LoadSites()
	if err != nil || reg == nil {
		return
	}
	suffix := "." + tld
	for _, s := range reg.Sites {
		onTLD := false
		for _, d := range s.Domains {
			if strings.HasSuffix(d, suffix) {
				onTLD = true
				break
			}
		}
		if !onTLD {
			continue
		}
		if enabling {
			if s.Secured || !(projectWantsHTTPS(s.Path) || s.SecuredBeforeDNSOff) {
				if s.SecuredBeforeDNSOff {
					s.SecuredBeforeDNSOff = false
					_ = config.AddSite(s)
				}
				continue
			}
			s.Secured = true
			s.SecuredBeforeDNSOff = false
			if err := config.AddSite(s); err != nil {
				fmt.Printf("    WARN: %s: restore HTTPS: %v\n", s.Name, err)
				continue
			}
			if err := certs.ReissueCert(s); err != nil {
				fmt.Printf("    WARN: %s: reissue cert: %v\n", s.Name, err)
			}
			_ = envfile.SyncPrimaryDomain(s.Path, s.PrimaryDomain(), true)
			feedback.Note(fmt.Sprintf("%s: restored https://%s", s.Name, s.PrimaryDomain()))
		} else {
			if !s.Secured {
				continue
			}
			s.SecuredBeforeDNSOff = true
			s.Secured = false
			if err := config.AddSite(s); err != nil {
				fmt.Printf("    WARN: %s: drop HTTPS: %v\n", s.Name, err)
				continue
			}
			removeStaleCerts(s.PrimaryDomain())
			_ = envfile.SyncPrimaryDomain(s.Path, s.PrimaryDomain(), false)
			feedback.Note(fmt.Sprintf("%s: dropped to http://%s (HTTPS unavailable with DNS off)", s.Name, s.PrimaryDomain()))
		}
	}
}

// removeStaleVhosts deletes the old <domain>.conf and <domain>-ssl.conf so
// the next regen pass does not have two vhosts (old + new) competing for the
// same upstream. Errors are swallowed: missing files are the common case.
func removeStaleVhosts(oldDomain string) {
	if oldDomain == "" {
		return
	}
	for _, suffix := range []string{".conf", "-ssl.conf"} {
		_ = os.Remove(filepath.Join(config.NginxConfD(), oldDomain+suffix))
	}
}

// removeStaleCerts deletes the .crt and .key for a site whose TLS state was
// flipped off as part of the migration (HTTPS unavailable in disabled-DNS
// mode). Mirrors the cleanup UnsecureSite does but for the OLD primary so we
// do not leave dead cert files under the previous TLD on disk.
func removeStaleCerts(oldDomain string) {
	if oldDomain == "" {
		return
	}
	dir := filepath.Join(config.CertsDir(), "sites")
	for _, ext := range []string{".crt", ".key"} {
		_ = os.Remove(filepath.Join(dir, oldDomain+ext))
	}
}
