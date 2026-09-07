package cli

import (
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// servlo link (secured project) and servlo setup call runSecure with a nil
// *cobra.Command, so it must not panic reading the --renew flag; it should just
// proceed to the secure toggle, which errors cleanly on an unknown site.
func TestRunSecure_NilCommandDoesNotPanic(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	if err := runSecure(nil, []string{"nonexistent-site-xyz"}); err == nil {
		t.Fatal("expected an error for an unknown site, got nil")
	}
}

// `servlo sites` prints the name and the domain side by side, and the domain is
// the wider, more memorable column — so it is the one an operator types. It did
// not work: every command built on resolveSiteName took the name only, and
// answered "site not found — run 'servlo link' first" on a site that was linked
// and serving. This is the whole class, since the seven commands share the
// helper.
func TestResolveSiteNameAcceptsEitherIdentifier(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if err := config.AddSite(config.Site{
		Name:    "astrolov",
		Domains: []string{"astrolov.com", "admin.astrolov.com"},
		Path:    filepath.Join(tmp, "sites", "astrolov.com"),
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	for _, ref := range []string{"astrolov", "astrolov.com", "admin.astrolov.com"} {
		got, err := resolveSiteName([]string{ref})
		if err != nil {
			t.Fatalf("resolveSiteName(%q): %v", ref, err)
		}
		if got != "astrolov" {
			t.Errorf("resolveSiteName(%q) = %q, want the registered name %q", ref, got, "astrolov")
		}
	}

	// A reference that matches nothing comes back untouched, so the caller's
	// own "not found" is what the operator reads rather than a silent rewrite.
	got, err := resolveSiteName([]string{"not-a-site"})
	if err != nil {
		t.Fatalf("resolveSiteName on an unknown ref: %v", err)
	}
	if got != "not-a-site" {
		t.Errorf("an unknown reference should pass through, got %q", got)
	}
}
