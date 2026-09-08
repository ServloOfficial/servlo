package watcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	oldRenew, oldReload := renewIfDue, reloadNginx
	renewIfDue = fn
	reloadNginx = func() error { reloads++; return nil }
	t.Cleanup(func() { renewIfDue, reloadNginx = oldRenew, oldReload })
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
