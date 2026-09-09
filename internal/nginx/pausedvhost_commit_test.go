package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The third one.
//
// The panel's two vhosts were the exception §3.4 admits none of, and they were
// brought inside commitVhost. The paused vhost is written the same way and was
// left behind: a bare os.WriteFile into conf.d/<domain>.conf, no validation, no
// backup, nothing to roll back to.
//
// It matters more than the panel's did. Pausing a site happens on a running
// machine with every other site live, and the caller reloads nginx immediately
// afterwards. nginx loads its whole configuration or none of it, so a paused
// vhost it refuses does not take the paused site down, it takes down every site
// on the server, and there is no previous file to put back.
func pausedSite() config.Site {
	return config.Site{Name: "acme", Domains: []string{"acme.example.com"}, Path: "/srv/acme"}
}

func TestPausedVhostIsValidatedAndBackedUp(t *testing.T) {
	isolateNginx(t)

	var asked int
	restore := swapVhostTest(func() (string, error) { asked++; return "ok", nil })
	defer restore()

	if err := GeneratePausedVhost(pausedSite()); err != nil {
		t.Fatal(err)
	}
	if asked == 0 {
		t.Error("pausing a site wrote its vhost without asking nginx whether the config still loads")
	}

	// Unpausing and pausing again replaces a file, so there is something to
	// keep a copy of.
	if err := GenerateVhost(pausedSite(), "8.4"); err != nil {
		t.Fatalf("regenerating the live vhost: %v", err)
	}
	asked = 0
	if err := GeneratePausedVhost(pausedSite()); err != nil {
		t.Fatal(err)
	}
	if asked == 0 {
		t.Error("replacing a live vhost with the paused one asked nginx nothing")
	}
	backups, err := os.ReadDir(config.NginxConfDBkp())
	if err != nil || len(backups) == 0 {
		t.Errorf("pausing kept no backup of the vhost it replaced (err %v)", err)
	}
}

// A paused vhost nginx refuses must not be left on disk. Rolling back is the
// whole reason for asking, and here the file being replaced is the live site's
// own vhost.
func TestPausedVhostRollsBackWhenNginxRefusesIt(t *testing.T) {
	isolateNginx(t)

	restore := swapVhostTest(func() (string, error) { return "ok", nil })
	if err := GenerateVhost(pausedSite(), "8.4"); err != nil {
		t.Fatal(err)
	}
	restore()

	confPath := filepath.Join(config.NginxConfD(), "acme.example.com.conf")
	before, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}

	restore = swapVhostTest(func() (string, error) {
		return "nginx: [emerg] something in acme.example.com.conf is wrong", errRefused
	})
	defer restore()

	if err := GeneratePausedVhost(pausedSite()); err == nil {
		t.Error("pausing reported success on a vhost nginx refused")
	}

	after, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("the live vhost is gone after a refused pause: %v", err)
	}
	if string(after) != string(before) {
		t.Error("a refused paused vhost was left on disk over the live one")
	}
	if strings.Contains(string(after), "paused.html") {
		t.Error("the refused paused page is still being served")
	}
}
