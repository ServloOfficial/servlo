package siteops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/fpmpool"
	"github.com/realrashid/servlo/internal/podman"
)

// poolHome points the data directory at a scratch home and records what
// SyncFPMPool asked to be reloaded, so these tests never signal a container.
func poolHome(t *testing.T) *[]string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")

	var reloaded []string
	prev := reloadFPM
	reloadFPM = func(container string) error {
		reloaded = append(reloaded, container)
		return nil
	}
	t.Cleanup(func() { reloadFPM = prev })
	return &reloaded
}

func fpmSite() config.Site {
	return config.Site{
		Name:       "example-com",
		Domains:    []string{"example.com"},
		Path:       "/home/servlo/sites/example.com",
		PHPVersion: "8.4",
	}
}

func TestSyncFPMPool_WritesThePoolWhereTheSitesOwnContainerReadsIt(t *testing.T) {
	poolHome(t)
	site := fpmSite()

	if err := SyncFPMPool(site); err != nil {
		t.Fatalf("SyncFPMPool: %v", err)
	}

	dir := config.FPMPoolDir(podman.FPMContainerName(site, site.PHPVersion))
	body, err := os.ReadFile(fpmpool.Path(dir, site.Name))
	if err != nil {
		t.Fatalf("no pool for the site: %v", err)
	}
	if !strings.Contains(string(body), "[example-com]") {
		t.Errorf("the pool is not the site's:\n%s", body)
	}
	if !strings.Contains(string(body), "chdir = "+site.Path) {
		t.Errorf("the pool does not chdir into the site:\n%s", body)
	}
}

// The vhost switches to the socket only once the pool file is there, so a pool
// written after the vhost leaves the site on the shared container until
// something else regenerates it.
func TestSyncFPMPool_HappensBeforeTheVhostIsGenerated(t *testing.T) {
	poolHome(t)
	site := fpmSite()

	dir := config.FPMPoolDir(podman.FPMContainerName(site, site.PHPVersion))
	if err := SyncFPMPool(site); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fpmpool.Path(dir, site.Name)); err != nil {
		t.Fatalf("the pool is not on disk when the vhost would be rendered: %v", err)
	}
}

// A pool changes nothing until the master re-reads it, and re-reading has to be
// a reload rather than a restart or one site's settings change drops every
// other site's in-flight requests.
func TestSyncFPMPool_ReloadsTheMasterThatOwnsThePool(t *testing.T) {
	reloaded := poolHome(t)
	site := fpmSite()

	if err := SyncFPMPool(site); err != nil {
		t.Fatal(err)
	}

	want := podman.FPMContainerName(site, site.PHPVersion)
	if len(*reloaded) != 1 || (*reloaded)[0] != want {
		t.Errorf("reloaded %v, want just %q", *reloaded, want)
	}
}

// A site served by something other than FPM has no pool to write, and writing
// one would define a pool listening on a socket nothing ever asks for.
func TestSyncFPMPool_SkipsSitesThatAreNotServedByFPM(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*config.Site)
	}{
		{"frankenphp", func(s *config.Site) { s.Runtime = "frankenphp" }},
		{"custom container", func(s *config.Site) { s.ContainerPort = 3000 }},
		{"host proxy", func(s *config.Site) { s.HostPort = 5173 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reloaded := poolHome(t)
			site := fpmSite()
			tc.apply(&site)

			if err := SyncFPMPool(site); err != nil {
				t.Fatalf("SyncFPMPool: %v", err)
			}

			root := config.FPMPoolRoot()
			if entries, err := os.ReadDir(root); err == nil {
				for _, e := range entries {
					inner, _ := os.ReadDir(filepath.Join(root, e.Name()))
					if len(inner) > 0 {
						t.Errorf("a pool was written for a site FPM does not serve: %s/%s", e.Name(), inner[0].Name())
					}
				}
			}
			if len(*reloaded) != 0 {
				t.Errorf("reloaded %v for a site with no pool", *reloaded)
			}
		})
	}
}

// A site handle that could name a path decides a pool name and a socket
// filename. nginx already falls back to the shared container for one, so
// writing a pool it will never point at is the half that has to agree.
func TestSyncFPMPool_SkipsAHandleThatCouldNameAPath(t *testing.T) {
	poolHome(t)
	site := fpmSite()
	site.Name = "my_site"

	if err := SyncFPMPool(site); err != nil {
		t.Fatalf("SyncFPMPool: %v", err)
	}

	dir := config.FPMPoolDir(podman.FPMContainerName(site, site.PHPVersion))
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		t.Errorf("a pool was written for an unusable handle: %v", entries[0].Name())
	}
}

// A site's pool has to go when the site does, or the master keeps a pool
// chdir'd into a directory that no longer exists and keeps a socket bound that
// the next site to take that name would inherit.
func TestRemoveFPMPool_TakesThePoolFromEveryContainer(t *testing.T) {
	reloaded := poolHome(t)

	// The same handle in two containers' directories, which is what a PHP
	// version switch leaves behind mid-move.
	for _, unit := range []string{"servlo-php83-fpm", "servlo-php84-fpm"} {
		if _, err := fpmpool.Write(config.FPMPoolDir(unit), fpmpool.Settings{
			Site: "example-com", Root: "/srv/example", SocketDir: config.FPMSocketDir(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveFPMPool("example-com"); err != nil {
		t.Fatalf("RemoveFPMPool: %v", err)
	}

	for _, unit := range []string{"servlo-php83-fpm", "servlo-php84-fpm"} {
		if _, err := os.Stat(fpmpool.Path(config.FPMPoolDir(unit), "example-com")); !os.IsNotExist(err) {
			t.Errorf("the pool survived in %s", unit)
		}
	}
	// Both masters have to be told, or one keeps serving a site that is gone.
	if len(*reloaded) != 2 {
		t.Errorf("reloaded %v, want both containers", *reloaded)
	}
}

// Removing a pool for a site that never had one is the ordinary case on an
// install that predates them.
// A reload is cheap but not free, and a container that lost nothing has
// nothing to re-read.
func TestRemoveFPMPool_IsQuietWhenThereIsNothingToRemove(t *testing.T) {
	reloaded := poolHome(t)

	// A container with pools in it, none of them this site's.
	if _, err := fpmpool.Write(config.FPMPoolDir("servlo-php84-fpm"), fpmpool.Settings{
		Site: "other-site", Root: "/srv/other", SocketDir: config.FPMSocketDir(),
	}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveFPMPool("example-com"); err != nil {
		t.Errorf("RemoveFPMPool on a site with no pool: %v", err)
	}
	if len(*reloaded) != 0 {
		t.Errorf("reloaded %v when nothing changed", *reloaded)
	}
	if _, err := os.Stat(fpmpool.Path(config.FPMPoolDir("servlo-php84-fpm"), "other-site")); err != nil {
		t.Errorf("another site's pool was removed: %v", err)
	}
}

// A PHP version change moves the site between containers. The pool it left
// behind would keep the old master binding the socket the new one needs.
func TestSyncFPMPool_LeavesNoPoolBehindInTheOldVersionsContainer(t *testing.T) {
	poolHome(t)
	site := fpmSite()

	site.PHPVersion = "8.3"
	if err := SyncFPMPool(site); err != nil {
		t.Fatal(err)
	}
	site.PHPVersion = "8.4"
	if err := SyncFPMPool(site); err != nil {
		t.Fatal(err)
	}

	old := fpmpool.Path(config.FPMPoolDir("servlo-php83-fpm"), site.Name)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("the 8.3 pool survived the move to 8.4, so both masters bind the site's socket")
	}
	current := fpmpool.Path(config.FPMPoolDir("servlo-php84-fpm"), site.Name)
	if _, err := os.Stat(current); err != nil {
		t.Errorf("no pool in the new version's container: %v", err)
	}
}
