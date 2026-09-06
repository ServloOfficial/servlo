package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/fpmpool"
	"github.com/ServloOfficial/servlo/internal/podman"
)

// isolate points every servlo path at a temp dir so a test never reads or
// writes the developer's real config.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
}

func fpmSite() config.Site {
	return config.Site{
		Name:       "ci-one-example",
		Path:       "/home/someone/sites/ci-one.example",
		Domains:    []string{"ci-one.example"},
		PHPVersion: "8.5",
	}
}

// A site restored onto a fresh server has its registry entry and its files, but
// none of the generated files that make it answerable: `servlo restore` writes
// neither, and `servlo start` regenerated quadlets, services and workers while
// leaving these two out. The result was nginx serving its not-found page for
// every restored site and PHP-FPM failing to start at all, because a pool
// directory with no pool in it is a config error php-fpm exits on.
func TestSiteServingFilesMissing_ReportsAFreshlyRestoredSite(t *testing.T) {
	isolate(t)

	if !siteServingFilesMissing(fpmSite()) {
		t.Error("a site with no vhost and no pool was reported as ready to serve")
	}
}

// Both files present is the ordinary case, and regenerating on every start
// would rewrite every site's vhost for no reason.
func TestSiteServingFilesMissing_LeavesACompleteSiteAlone(t *testing.T) {
	isolate(t)
	s := fpmSite()

	if err := os.MkdirAll(config.NginxConfD(), 0o755); err != nil {
		t.Fatal(err)
	}
	vhost := filepath.Join(config.NginxConfD(), s.PrimaryDomain()+".conf")
	if err := os.WriteFile(vhost, []byte("server {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	poolDir := config.FPMPoolDir(podman.FPMContainerName(s, s.PHPVersion))
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fpmpool.Path(poolDir, s.Name), []byte("[pool]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if siteServingFilesMissing(s) {
		t.Error("a site with both its vhost and its pool was reported as missing them")
	}
}

// The vhost alone is not enough. A pool lost on its own still leaves php-fpm
// refusing to start, and the vhost pointing at a socket nothing listens on.
func TestSiteServingFilesMissing_ReportsAMissingPoolOnItsOwn(t *testing.T) {
	isolate(t)
	s := fpmSite()

	if err := os.MkdirAll(config.NginxConfD(), 0o755); err != nil {
		t.Fatal(err)
	}
	vhost := filepath.Join(config.NginxConfD(), s.PrimaryDomain()+".conf")
	if err := os.WriteFile(vhost, []byte("server {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !siteServingFilesMissing(s) {
		t.Error("a site whose pool is gone was reported as ready to serve")
	}
}

// A site served by its own container has no pool in the shared FPM directory,
// so looking for one would regenerate it on every single start.
func TestSiteServingFilesMissing_DoesNotExpectAPoolForAContainerSite(t *testing.T) {
	isolate(t)
	s := fpmSite()
	s.ContainerPort = 8080

	if err := os.MkdirAll(config.NginxConfD(), 0o755); err != nil {
		t.Fatal(err)
	}
	vhost := filepath.Join(config.NginxConfD(), s.PrimaryDomain()+".conf")
	if err := os.WriteFile(vhost, []byte("server {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if siteServingFilesMissing(s) {
		t.Error("a custom-container site with a vhost was reported as missing a pool it never has")
	}
}
