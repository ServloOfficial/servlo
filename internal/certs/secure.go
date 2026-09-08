package certs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

// SecureSite issues a TLS certificate for the site and switches its nginx vhost to HTTPS.
func SecureSite(site config.Site) error {
	if err := issueSiteCert(site); err != nil {
		return fmt.Errorf("issuing certificate: %w", err)
	}

	if site.IsHostProxy() {
		if err := nginx.GenerateHostProxySSLVhost(site); err != nil {
			return fmt.Errorf("generating host-proxy SSL vhost: %w", err)
		}
	} else if site.IsCustomContainer() {
		if err := nginx.GenerateCustomSSLVhost(site); err != nil {
			return fmt.Errorf("generating custom SSL vhost: %w", err)
		}
	} else if site.IsFrankenPHP() {
		if err := nginx.GenerateFrankenPHPSSLVhost(site); err != nil {
			return fmt.Errorf("generating FrankenPHP SSL vhost: %w", err)
		}
	} else if err := nginx.GenerateSSLVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating SSL vhost: %w", err)
	}

	sslConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+"-ssl.conf")
	mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing HTTP vhost: %w", err)
	}
	if err := os.Rename(sslConf, mainConf); err != nil {
		return fmt.Errorf("renaming SSL config: %w", err)
	}

	return nil
}

// ReissueCert forces a fresh certificate for the site, covering its primary
// domain and every alias. Call it after the site's domain set changes, so the
// SANs and the vhost agree again.
func ReissueCert(site config.Site) error {
	return issueSiteCert(site)
}

// RenewIfDue reissues the site's certificate when it has drifted inside the
// reissue window, and leaves a healthy one alone. Unlike ReissueCert it never
// forces, so it is cheap to call on every boot and every watcher pass, which is
// what keeps a long-lived secured site from quietly serving an expired leaf.
//
// It reports whether it actually reissued, because nginx goes on serving the
// certificate it loaded at its last reload: a renewal nobody reloads for is a
// new file on disk and an old certificate on the wire.
func RenewIfDue(site config.Site) (renewed bool, err error) {
	certsDir, domains := siteCertDomains(site)
	if !NeedsRenewal(site) {
		return false, nil
	}
	if err := IssueCertForce(site.PrimaryDomain(), domains, certsDir); err != nil {
		return false, err
	}
	return true, nil
}

// NeedsRenewal reports whether the site's certificate is missing, unreadable,
// expired, or close enough to expiry to be reissued now.
func NeedsRenewal(site config.Site) bool {
	certsDir, _ := siteCertDomains(site)
	certFile := filepath.Join(certsDir, site.PrimaryDomain()+".crt")
	keyFile := filepath.Join(certsDir, site.PrimaryDomain()+".key")
	if _, err := os.Stat(keyFile); err != nil {
		return true
	}
	return certNeedsReissue(certFile, certReissueWindow)
}

// siteCertDomains assembles the cert output directory and the SAN list shared by
// the reuse and force reissue paths so they cannot drift.
func siteCertDomains(site config.Site) (certsDir string, domains []string) {
	certsDir = filepath.Join(config.CertsDir(), "sites")
	domains = make([]string, len(site.Domains))
	copy(domains, site.Domains)
	return certsDir, domains
}

// issueSiteCert issues the site's certificate atomically: a transient issuance
// failure leaves the existing cert intact rather than tripping RepairVhosts into
// flipping the site to HTTP.
func issueSiteCert(site config.Site) error {
	certsDir, domains := siteCertDomains(site)
	return IssueCertForce(site.PrimaryDomain(), domains, certsDir)
}

// UnsecureSite regenerates a plain HTTP vhost for the site, removing TLS.
func UnsecureSite(site config.Site) error {
	mainConf := filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf")
	if err := os.Remove(mainConf); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing SSL vhost: %w", err)
	}

	if site.IsHostProxy() {
		if err := nginx.GenerateHostProxyVhost(site); err != nil {
			return fmt.Errorf("generating host-proxy HTTP vhost: %w", err)
		}
	} else if site.IsCustomContainer() {
		if err := nginx.GenerateCustomVhost(site); err != nil {
			return fmt.Errorf("generating custom HTTP vhost: %w", err)
		}
	} else if site.IsFrankenPHP() {
		if err := nginx.GenerateFrankenPHPVhost(site); err != nil {
			return fmt.Errorf("generating FrankenPHP HTTP vhost: %w", err)
		}
	} else if err := nginx.GenerateVhost(site, site.PHPVersion); err != nil {
		return fmt.Errorf("generating HTTP vhost: %w", err)
	}

	// Remove cert files
	certsDir := filepath.Join(config.CertsDir(), "sites")
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".crt")) //nolint:errcheck
	os.Remove(filepath.Join(certsDir, site.PrimaryDomain()+".key")) //nolint:errcheck

	return nil
}
