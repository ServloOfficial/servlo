package watcher

import (
	"log"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
)

// Certificate renewal, and why it is a watcher pass rather than a command.
//
// A Let's Encrypt leaf is good for ninety days. Everything servlo needs to
// renew one has been here from the start — the reissue window, the atomic swap
// that keeps a complete certificate on disk at every instant, the failure
// record that survives a restart, the alert, the doctor check — and nothing
// called it. certs had a function written and tested for exactly this, doc
// comment naming the boot and watcher passes that would use it, and no caller
// anywhere. `servlo secure --renew`, typed by an operator per site, was the
// whole renewal story.
//
// That is the failure PRD section 3.3 is written against: a site does not
// silently serve an expired certificate. Ninety days after an install, every
// site on the machine goes down at once, and the only warning was a doctor
// line nobody had a reason to run.

// CertRenewalInterval is how often the sweep runs. The reissue window is thirty
// days wide, so twice a day is many chances to catch a certificate entering it
// and no load worth measuring: a site whose certificate is healthy costs one
// stat and one parse.
const CertRenewalInterval = 12 * time.Hour

// Seams. Both reach the network or the service manager, so a test that could
// not replace them could only run on a server.
var (
	renewIfDue  = certs.RenewIfDue
	reloadNginx = nginx.Reload
)

// WatchCertRenewal renews the certificates of secured sites as they age.
func WatchCertRenewal(interval time.Duration) {
	// Once at startup as well as on the tick. A machine that was off for a
	// month, or restored from a backup, comes back with certificates that aged
	// while nothing was watching them, and waiting half a day to look would be
	// half a day of serving something expired.
	renewCertsOnce()

	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		renewCertsOnce()
	}
}

// renewCertsOnce is one sweep over the secured sites.
func renewCertsOnce() {
	reg, err := config.LoadSites()
	if err != nil {
		return
	}

	renewed := 0
	for _, site := range reg.Sites {
		if !site.Secured || site.Ignored {
			continue
		}
		ok, err := renewIfDue(site)
		if err != nil {
			// Said once here and nowhere else in this function: the failure is
			// already recorded, alerted and surfaced in doctor and the panel by
			// the issuance itself. The sweep keeps going, because one domain
			// whose DNS has moved must not stop the renewal of the others.
			log.Printf("[certs] renewing %s failed: %v", site.PrimaryDomain(), err)
			continue
		}
		if ok {
			log.Printf("[certs] renewed %s", site.PrimaryDomain())
			renewed++
		}
	}

	// Once, after the whole sweep, and only if something changed. nginx serves
	// the certificate it loaded at its last reload, so a renewal nobody reloads
	// for is a new file on disk and the old certificate still on the wire,
	// expiring on its original schedule.
	if renewed > 0 {
		if err := reloadNginx(); err != nil {
			log.Printf("[certs] renewed %d certificate(s) but nginx did not reload, so the old ones are still being served: %v", renewed, err)
		}
	}
}
