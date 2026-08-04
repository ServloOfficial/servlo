package tui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/siteinfo"
)

func TestSiteTabsHeader_HighlightsActive(t *testing.T) {
	base := []siteTab{tabSiteOverview, tabSiteLogs, tabSiteEnv}
	for _, tab := range base {
		got := stripANSI(siteTabsHeader(tab, base))
		want := siteTabLabel(tab)
		if !strings.Contains(got, want) {
			t.Errorf("active=%v: expected label %q in %q", tab, want, got)
		}
	}
}

func TestAvailableSiteTabs_DoctorForEveryFramework(t *testing.T) {
	plain := availableSiteTabs(&siteinfo.EnrichedSite{Name: "static", FrameworkName: "wordpress"})
	if !slices.Contains(plain, tabSiteDoctor) {
		t.Errorf("every framework should offer the Doctor tab, got %v", plain)
	}
	laravel := availableSiteTabs(&siteinfo.EnrichedSite{Name: "app", FrameworkName: "laravel"})
	if !slices.Contains(laravel, tabSiteDoctor) {
		t.Errorf("Laravel site should offer the Doctor tab, got %v", laravel)
	}
	// Doctor is the fourth tab, so the strip numbers it [4].
	if got := stripANSI(siteTabsHeader(tabSiteOverview, laravel)); !strings.Contains(got, "[4] Doctor") {
		t.Errorf("strip should carry [4] Doctor, got %q", got)
	}
}

func TestSiteEnvContent_ShowsFileContents(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("APP_KEY=abc\nDB_PASS=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: dir}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "APP_KEY=abc") || !strings.Contains(joined, "DB_PASS=secret") {
		t.Errorf("expected env contents in output:\n%s", joined)
	}
}

func TestSiteEnvContent_MissingFileShowsHint(t *testing.T) {
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: t.TempDir()}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "no .env on disk") {
		t.Errorf("expected missing-env hint:\n%s", joined)
	}
}

func TestSiteEnvContent_EmptyFileShowsHint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewModel("test")
	site := &siteinfo.EnrichedSite{Name: "acme", Path: dir}
	lines := siteEnvContentLines(m, site, 120)
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "empty") {
		t.Errorf("expected empty-env hint:\n%s", joined)
	}
}
