package podman

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// The FPM quadlet mounts the pool directory over /usr/local/etc/php-fpm.d, so
// the directory existing is not the same as FPM having a pool: the mount hides
// the image's own. An empty directory means php-fpm starts, finds nothing to
// serve, exits, and systemd restarts it forever.
//
// That is what a fresh install did. It has no sites, so nothing wrote a pool,
// so `servlo install` finished with `✓ installation complete` over a container
// that was crash-looping, and `servlo php`, `servlo composer` and every other
// shim failed until the operator happened to link a first site. It was invisible
// because every job that exercised servlo created a site as its next step.
func TestEnsureFPMMountsLeavesThePoolDirectoryNonEmpty(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	if err := ensureFPMMounts("8.5"); err != nil {
		t.Fatalf("ensureFPMMounts: %v", err)
	}

	dir := config.FPMPoolDir(SharedFPMContainerName("8.5"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading the pool directory: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("the pool directory %s is empty, so php-fpm has no pool and will not stay up", dir)
	}
}
