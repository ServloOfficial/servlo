package appinstall

import (
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// An installed app has to be registered against a real PHP version.
//
// It was registered against the empty string, because the Site literal never
// set the field and nothing filled it in. Everything downstream takes the
// version as a name: the pool went to fpm-pools/servlo-php-fpm rather than
// fpm-pools/servlo-php85-fpm, so the container that actually runs never mounted
// it; the vhost found no pool for the site and so wired no PHP upstream, and
// nginx served the application's own installer as a static file, answering the
// POST that drives it with 405. A quadlet for "servlo-php-fpm" was written too,
// leaving a dead unit named "Servlo PHP  FPM" with the gap where its version
// should be.
//
// Every one-click install produced a site that could not run PHP at all.
func TestInstallRegistersTheSiteWithAResolvedPHPVersion(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	if _, err := Install(t.Context(), Options{
		App:    "example",
		Domain: "blog.example.com",
		Path:   filepath.Join(t.TempDir(), "site"),
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if c.registeredPHP == "" {
		t.Error("the app was registered with an empty PHP version, so its pool and quadlet are named after a container that does not exist")
	}
	if c.registeredSite.PHPVersion == "" {
		t.Error("the registered site carries no PHP version, so nothing downstream can name its container")
	}
	if c.registeredSite.PHPVersion != c.registeredPHP {
		t.Errorf("the site says PHP %q and the link was told %q; they name the same container and must agree",
			c.registeredSite.PHPVersion, c.registeredPHP)
	}
}

// An installed app has to be in the site registry, and it was not in it at all.
//
// The installer writes every artifact by hand through siteops.FinishLink — the
// pool, the vhost, the quadlet — but registering the site is not one of those:
// `servlo link` does that a layer up, in linker.Apply, which this path does not
// go through. So an install left a site that nginx served and nothing else knew
// about. It was absent from `servlo sites` and from the panel, and every feature
// that walks the registry walked straight past it: no backups, no scheduled
// cron, and `servlo secure` could not find the domain to issue a certificate
// for.
func TestInstallPutsTheSiteInTheRegistry(t *testing.T) {
	sandbox(t)
	stub(t, withSetup())

	if _, err := Install(t.Context(), Options{
		App:    "example",
		Domain: "blog.example.com",
		Path:   filepath.Join(t.TempDir(), "site"),
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if _, err := config.FindSiteByDomain("blog.example.com"); err != nil {
		t.Fatalf("the installed app is not in the site registry, so nothing but nginx knows it exists: %v", err)
	}
}

// The registry entry has to carry the document root as well. The linker path
// sets it; this one did not, and an omission is not a default: the panel, the
// deploy and the doctor all read PublicDir straight off the registry, so a site
// with an empty one cannot tell any of them where its code lives.
func TestInstallRecordsTheDocumentRoot(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	if _, err := Install(t.Context(), Options{
		App:    "example",
		Domain: "blog.example.com",
		Path:   filepath.Join(t.TempDir(), "site"),
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if c.registeredSite.PublicDir == "" {
		t.Error("the installed site records no document root, so nothing that reads the registry knows where to serve from")
	}
}
