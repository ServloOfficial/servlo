package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// TestPersistPHPVersion_PlainSitePinsTheSiteRoot keeps the ordinary case working:
// from a subdirectory of a registered site the pin belongs at the project root.
func TestPersistPHPVersion_PlainSitePinsTheSiteRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)

	site := filepath.Join(tempRoot(t), "app")
	sub := filepath.Join(site, "app", "Http")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{Name: "app", Path: site, PHPVersion: "8.5"}); err != nil {
		t.Fatal(err)
	}

	persistPHPVersion(sub, "8.3")

	assertPinFile(t, site, "8.3")
}

// tempRoot is t.TempDir() with symlinks resolved. AddSite canonicalises a site's
// path, and on macOS the temp dir sits under /var, a symlink to /private/var, so
// an unresolved path would never prefix-match the registered site.
func tempRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertPinFile(t *testing.T, dir, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, ".php-version"))
	if err != nil {
		t.Fatalf("reading .php-version in %s: %v", dir, err)
	}
	if got := strings.TrimSpace(string(data)); got != want {
		t.Errorf(".php-version = %q, want %q", got, want)
	}
}
