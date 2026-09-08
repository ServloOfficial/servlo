package appinstall

import (
	"path/filepath"
	"testing"
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
