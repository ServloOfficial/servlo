package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/fpmpool"
)

// Each site's pool lives in the directory belonging to the FPM container that
// serves it, and each container's master defines every pool it can see. So a
// version switch has to move the pool, not just the vhost: left behind, the old
// version's master still defines the site and still binds the socket the new
// one needs, and the new container has no pool for the site at all.
func TestSetSitePHPVersion_MovesThePoolToTheNewVersionsContainer(t *testing.T) {
	site := phpVersionTestSite(t, asFPM)
	stubPHPVersionDeps(t, "", "")
	poolSignals(t)

	if err := SyncFPMPool(*site); err != nil {
		t.Fatalf("initial pool: %v", err)
	}
	from := "servlo-php" + strings.ReplaceAll(site.PHPVersion, ".", "") + "-fpm"
	if _, err := os.Stat(fpmpool.Path(config.FPMPoolDir(from), site.Name)); err != nil {
		t.Fatalf("the site had no pool to move: %v", err)
	}

	if _, err := SetSitePHPVersion(site, "8.2"); err != nil {
		t.Fatalf("SetSitePHPVersion: %v", err)
	}

	if _, err := os.Stat(fpmpool.Path(config.FPMPoolDir("servlo-php82-fpm"), site.Name)); err != nil {
		t.Errorf("no pool in the new version's container, so the site falls back to the shared one: %v", err)
	}
	if _, err := os.Stat(fpmpool.Path(config.FPMPoolDir(from), site.Name)); !os.IsNotExist(err) {
		t.Errorf("the pool survived in %s, so both masters bind the site's socket", from)
	}
}

// And the vhost written by the same switch has to point at that pool. The two
// are decided separately (the vhost checks whether the pool file is there), so
// a switch that wrote the vhost before moving the pool would leave the site on
// the shared container until something else regenerated it.
func TestSetSitePHPVersion_PointsTheRewrittenVhostAtTheMovedPool(t *testing.T) {
	site := phpVersionTestSite(t, asFPM)
	stubPHPVersionDeps(t, "", "")
	poolSignals(t)

	if err := SyncFPMPool(*site); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSitePHPVersion(site, "8.2"); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(filepath.Join(config.NginxConfD(), site.PrimaryDomain()+".conf"))
	if err != nil {
		t.Fatalf("reading the vhost: %v", err)
	}
	want := "fastcgi_pass unix:" + fpmpool.SocketPath(config.FPMSocketDir(), site.Name) + ";"
	if !strings.Contains(string(body), want) {
		t.Errorf("the vhost does not pass to the site's own socket:\nwant %q\ngot:\n%s", want, body)
	}
}

// poolSignals keeps the pool sync from signalling a container.
func poolSignals(t *testing.T) {
	t.Helper()
	prev := reloadFPM
	reloadFPM = func(string) error { return nil }
	t.Cleanup(func() { reloadFPM = prev })
}
