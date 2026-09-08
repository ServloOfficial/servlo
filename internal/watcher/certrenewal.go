package watcher

import (
	"log"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/nginx"
	"github.com/ServloOfficial/servlo/internal/podman"
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

// nginxSettleWait bounds how long the first sweep waits for nginx before giving
// up on it. Generous, because the cost of being wrong in one direction is a
// certificate renewed a few hours late and in the other is a burnt validation.
const nginxSettleWait = 5 * time.Minute

// Seams. All three reach the network or the service manager, so a test that
// could not replace them could only run on a server.
var (
	renewIfDue        = certs.RenewIfDue
	reloadNginx       = nginx.Reload
	nginxIsUp         = func() bool { up, err := podman.ContainerRunning("servlo-nginx"); return err == nil && up }
	restoreInProgress = config.RestoreInProgress
	settleTick        = 5 * time.Second
	sweepDeadline     = func() time.Time { return time.Now().Add(nginxSettleWait) }
)

// WatchCertRenewal renews the certificates of secured sites as they age.
func WatchCertRenewal(interval time.Duration) {
	// Once at startup as well as on the tick. A machine that was off for a
	// month, or restored from a backup, comes back with certificates that aged
	// while nothing was watching them, and waiting half a day to look would be
	// half a day of serving something expired.
	//
	// But not before nginx is up. An HTTP-01 challenge is answered out of the
	// webroot nginx serves, so a sweep that runs first fails every renewal it
	// attempts, records a failure, raises an alert the operator has no cause
	// for, and spends one of the five validations Let's Encrypt allows per
	// hostname per hour. At boot the watcher is a systemd unit like any other
	// and nothing orders it after the nginx container.
	if waitForNginx() {
		renewCertsOnce()
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for range t.C {
		renewCertsOnce()
	}
}

// waitForNginx blocks until nginx is answering or the wait runs out, and
// reports whether it came up. A machine with no nginx serves no sites, so there
// is nothing there worth failing an issuance over.
func waitForNginx() bool {
	deadline := sweepDeadline()
	for {
		if nginxIsUp() {
			return true
		}
		if time.Now().After(deadline) {
			log.Printf("[certs] nginx did not come up within %s, so the startup renewal sweep is skipped", nginxSettleWait)
			return false
		}
		time.Sleep(settleTick)
	}
}

// renewCertsOnce is one sweep over the secured sites.
func renewCertsOnce() {
	// The same reason the startup sweep waits: an HTTP-01 challenge is answered
	// out of nginx's webroot, so attempting one while nginx is down turns a
	// healthy install into a recorded failure, an alert, and a validation spent
	// out of the five an hour the authority allows.
	if !nginxIsUp() {
		log.Print("[certs] nginx is not running, so this renewal sweep is skipped")
		return
	}

	// Nor during a rebuild. Certificates are not in a backup, so every secured
	// site on a restored server has none, and every one of them looks overdue
	// at once. The restore has already told the operator to point DNS here and
	// run servlo secure; sweeping now would answer that with an alert per site
	// for the thing they were just told to do next.
	if restoreInProgress() {
		log.Print("[certs] a restore is in progress, so certificates are left to the operator until it is done")
		return
	}

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
