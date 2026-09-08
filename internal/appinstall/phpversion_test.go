package appinstall

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
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

// The application's own installer is driven over HTTP against the site servlo
// just created, and nothing waited for that site to start answering.
//
// Between writing the pool and the vhost and reloading them there is a window
// where nginx has not picked up the server block and the FPM master has not
// finished spawning the pool's workers. A POST landing in it gets php-fpm's
// "File not found.", and the install reports a setup that did not complete over
// an application that was serving perfectly a second later. CI hit exactly that
// and then curled the same site successfully from the next step.
//
// What it leaves behind is what appinstall already refuses to leave behind when
// it cannot drive an installer at all: an uninstalled application on a live
// domain with its setup form open to the first passer-by.
func TestInstallWaitsForTheSiteBeforeDrivingItsInstaller(t *testing.T) {
	sandbox(t)
	c := stub(t, withSetup())

	if _, err := Install(t.Context(), Options{
		App:    "example",
		Domain: "blog.example.com",
		Path:   filepath.Join(t.TempDir(), "site"),
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if c.waitedFor == 0 {
		t.Error("the setup form was posted without waiting for the site to answer, which is the race that leaves an uninstalled app on a live domain")
	}
}

// A site that never answers must fail the install rather than post into the
// void: the operator needs to be told to finish the installer themselves.
func TestInstallReportsASiteThatNeverAnswers(t *testing.T) {
	sandbox(t)
	stub(t, withSetup())

	orig := waitForSiteFn
	waitForSiteFn = func(context.Context, string) error { return errors.New("no answer") }
	t.Cleanup(func() { waitForSiteFn = orig })

	_, err := Install(t.Context(), Options{
		App:    "example",
		Domain: "blog.example.com",
		Path:   filepath.Join(t.TempDir(), "site"),
	})
	if err == nil {
		t.Fatal("an install whose site never answered reported success")
	}
	if !strings.Contains(err.Error(), "setup could not be driven") {
		t.Errorf("the error should say the setup could not be driven, got %v", err)
	}
}
