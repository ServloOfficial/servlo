package certs

import (
	"os"

	"github.com/ServloOfficial/servlo/internal/auditlog"
)

// ForgetSite drops everything issuance left on disk for a domain: the
// certificate, its private key, the log of the last attempt, and any record
// that renewal was failing.
//
// The key is the part that matters. It is the one file a removed site must not
// leave behind, and nothing else on the machine ever deletes it.
//
// The failure record matters for a different reason. It is kept until an
// issuance succeeds, and a domain with no site behind it can never have one, so
// a record left here is a panel banner and a red line in doctor that no
// operator can act on and none of them can clear.
func ForgetSite(domain string) {
	if domain == "" {
		return
	}
	certPath, keyPath := SitePaths(domain)
	os.Remove(certPath)             //nolint:errcheck — nothing to remove is the common case
	os.Remove(keyPath)              //nolint:errcheck
	os.Remove(progressPath(domain)) //nolint:errcheck
	if dropFailure(domain) {
		auditlog.Record(auditlog.Entry{Action: "cert.forgotten", Subject: domain})
	}
}

// SitesDir is where every site's certificate and key live. Exported so the
// install that creates the directory and the panel that reads from it name the
// same place SitePaths writes to.
func SitesDir() string { return sitesDir() }
