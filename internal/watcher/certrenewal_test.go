package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ServloOfficial/servlo/internal/certs"
	"github.com/ServloOfficial/servlo/internal/config"
)

func writeSites(t *testing.T, sites string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	dir := filepath.Join(tmp, "servlo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sites.yaml"), []byte(sites), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stubRenewal(t *testing.T, fn func(config.Site) (bool, error)) *int {
	t.Helper()
	reloads := 0
	oldRenew, oldReload, oldUp, oldRestore := renewIfDue, reloadNginx, nginxIsUp, restoreInProgress
	renewIfDue = fn
	reloadNginx = func() error { reloads++; return nil }
	nginxIsUp = func() bool { return true }
	restoreInProgress = func() bool { return false }
	t.Cleanup(func() {
		renewIfDue, reloadNginx, nginxIsUp, restoreInProgress = oldRenew, oldReload, oldUp, oldRestore
	})
	return &reloads
}

const twoSecuredOnePlain = `sites:
  - name: alpha
    domains: [alpha.example]
    path: /srv/alpha
    secured: true
  - name: beta
    domains: [beta.example]
    path: /srv/beta
    secured: true
  - name: gamma
    domains: [gamma.example]
    path: /srv/gamma
`

// nginx serves the certificate it loaded at its last reload, so a renewal
// nobody reloads for leaves the old certificate on the wire, expiring on its
// original schedule.
func TestRenewCertsOnce_ReloadsOnceAfterRenewing(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	var asked []string
	reloads := stubRenewal(t, func(s config.Site) (bool, error) {
		asked = append(asked, s.PrimaryDomain())
		return true, nil
	})

	renewCertsOnce()

	if len(asked) != 2 {
		t.Errorf("asked about %v, want only the secured sites", asked)
	}
	if *reloads != 1 {
		t.Errorf("nginx reloaded %d times, want exactly one after the sweep", *reloads)
	}
}

// A sweep that renewed nothing is the ordinary case, twice a day, forever. It
// must not bounce nginx for it.
func TestRenewCertsOnce_DoesNotReloadWhenNothingRenewed(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	reloads := stubRenewal(t, func(config.Site) (bool, error) { return false, nil })

	renewCertsOnce()

	if *reloads != 0 {
		t.Errorf("nginx reloaded %d times for a sweep that renewed nothing", *reloads)
	}
}

// One domain whose DNS has moved must not stop the renewal of every other site
// on the machine.
func TestRenewCertsOnce_KeepsGoingPastAFailure(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	var asked []string
	reloads := stubRenewal(t, func(s config.Site) (bool, error) {
		asked = append(asked, s.PrimaryDomain())
		if s.PrimaryDomain() == "alpha.example" {
			return false, errors.New("domains do not resolve to this server")
		}
		return true, nil
	})

	renewCertsOnce()

	if len(asked) != 2 {
		t.Errorf("the sweep stopped after the failure; asked %v", asked)
	}
	if *reloads != 1 {
		t.Errorf("nginx reloaded %d times, want one for the site that did renew", *reloads)
	}
}

// An HTTP-01 challenge is answered out of nginx's webroot. Attempting a renewal
// with nginx down fails every one of them, records a failure, raises an alert
// the operator has no cause for, and spends one of the five validations the
// authority allows per hostname per hour. At boot the watcher is a systemd unit
// like any other and nothing orders it after the nginx container.
func TestRenewCertsOnce_SkipsTheSweepWhileNginxIsDown(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	asked := 0
	stubRenewal(t, func(config.Site) (bool, error) { asked++; return true, nil })
	nginxIsUp = func() bool { return false }

	renewCertsOnce()

	if asked != 0 {
		t.Errorf("the sweep tried %d renewals with nginx down; each one is a validation spent for nothing", asked)
	}
}

// A machine whose nginx never comes up serves no sites, so there is nothing
// there worth failing an issuance over. The wait has to end.
func TestWaitForNginx_GivesUp(t *testing.T) {
	oldUp, oldTick, oldDeadline := nginxIsUp, settleTick, sweepDeadline
	nginxIsUp = func() bool { return false }
	settleTick = time.Millisecond
	sweepDeadline = func() time.Time { return time.Now().Add(10 * time.Millisecond) }
	t.Cleanup(func() { nginxIsUp, settleTick, sweepDeadline = oldUp, oldTick, oldDeadline })

	if waitForNginx() {
		t.Error("waitForNginx reported nginx up when it never came up")
	}
}

func TestWaitForNginx_ReturnsOnceNginxAnswers(t *testing.T) {
	calls := 0
	oldUp, oldTick := nginxIsUp, settleTick
	nginxIsUp = func() bool { calls++; return calls >= 3 }
	settleTick = time.Millisecond
	t.Cleanup(func() { nginxIsUp, settleTick = oldUp, oldTick })

	if !waitForNginx() {
		t.Error("waitForNginx gave up on an nginx that came up")
	}
}

// Certificates are not in a backup, so every secured site on a restored server
// has none and every one of them looks overdue at once. The restore has just
// told the operator to point DNS here and run servlo secure; answering that
// with an alert per site for the thing they were told to do next is noise, and
// on a rebuild it is noise at the worst possible moment.
func TestRenewCertsOnce_LeavesARebuildToTheOperator(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	asked := 0
	stubRenewal(t, func(config.Site) (bool, error) { asked++; return true, nil })
	restoreInProgress = func() bool { return true }

	renewCertsOnce()

	if asked != 0 {
		t.Errorf("the sweep tried %d issuances during a restore, one alert each", asked)
	}
}

// writePanelDomain attaches a domain to the panel in the config the sweep
// reads, and optionally puts a certificate on disk for it, which is what tells
// the sweep the operator ever secured it.
func writePanelDomain(t *testing.T, domain string, secured bool) {
	t.Helper()
	dir := config.ConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("ui:\n  domain: "+domain+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !secured {
		return
	}
	certPath, keyPath := certs.SitePaths(domain)
	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{certPath, keyPath} {
		if err := os.WriteFile(p, []byte("stand-in"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// The panel's own domain is not a site, so the sweep over sites.yaml walks past
// it. Ninety days after `servlo panel domain secure` the panel is the one thing
// on the machine serving an expired certificate, and it is the thing the
// operator logs in to to find out.
func TestRenewCertsOnce_RenewsThePanelsOwnCertificate(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	writePanelDomain(t, "panel.example", true)
	var asked []string
	reloads := stubRenewal(t, func(s config.Site) (bool, error) {
		asked = append(asked, s.PrimaryDomain())
		return true, nil
	})

	renewCertsOnce()

	if !slices.Contains(asked, "panel.example") {
		t.Errorf("the panel's certificate was never renewed; asked about %v", asked)
	}
	if *reloads != 1 {
		t.Errorf("nginx reloaded %d times, want exactly one after the sweep", *reloads)
	}
}

// A domain attached but never secured has no certificate, and asking the
// authority for one nobody asked for spends a validation and raises a failure
// for a panel the operator is happily reaching by address.
func TestRenewCertsOnce_LeavesAnUnsecuredPanelDomainAlone(t *testing.T) {
	writeSites(t, twoSecuredOnePlain)
	writePanelDomain(t, "panel.example", false)
	var asked []string
	stubRenewal(t, func(s config.Site) (bool, error) {
		asked = append(asked, s.PrimaryDomain())
		return true, nil
	})

	renewCertsOnce()

	if slices.Contains(asked, "panel.example") {
		t.Errorf("the sweep tried to issue a certificate for a panel domain that has none: %v", asked)
	}
}
