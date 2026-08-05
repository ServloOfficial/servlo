package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/siteops"
)

func TestNginxShow_printsSavedOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if err := config.AddSite(config.Site{Name: "acme", Path: t.TempDir(), Domains: []string{"acme.test"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.NginxCustomD(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(config.NginxCustomD(), "acme.test.conf"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := siteops.ReadCustomNginx(resolveNginxTestDomain(t, "acme"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "# hi\n" || !got.Exists {
		t.Fatalf("got %+v", got)
	}
}

func resolveNginxTestDomain(t *testing.T, name string) string {
	t.Helper()
	_, domain, err := resolveNginxDomain([]string{name})
	if err != nil {
		t.Fatal(err)
	}
	return domain
}
