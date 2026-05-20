package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// writeSeedFile writes content to dir/rel, creating parent directories.
func writeSeedFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// notesContain reports whether any note contains sub.
func notesContain(notes []string, sub string) bool {
	for _, n := range notes {
		if strings.Contains(n, sub) {
			return true
		}
	}
	return false
}

// ── shared helpers ───────────────────────────────────────────────────────────

func TestSplitVersionedName(t *testing.T) {
	cases := []struct {
		in            string
		name, version string
	}{
		{"mariadb:11.8", "mariadb", "11.8"},
		{"mysql:8.0", "mysql", "8.0"},
		{"postgres", "postgres", ""},
		{"  redis:6  ", "redis", "6"},
		{"", "", ""},
	}
	for _, c := range cases {
		name, version := splitVersionedName(c.in)
		if name != c.name || version != c.version {
			t.Errorf("splitVersionedName(%q) = (%q, %q), want (%q, %q)", c.in, name, version, c.name, c.version)
		}
	}
}

func TestCollectDomains(t *testing.T) {
	cases := []struct {
		desc string
		in   []string
		want []string
	}{
		{"dedup and lowercase", []string{"Foo", "foo", "bar"}, []string{"foo", "bar"}},
		{"drop empty and wildcard", []string{"app", "", "*.app", "  "}, []string{"app"}},
		{"order preserved", []string{"c", "a", "b"}, []string{"c", "a", "b"}},
	}
	for _, c := range cases {
		got := collectDomains(c.in)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("[%s] collectDomains(%v) = %v, want %v", c.desc, c.in, got, c.want)
		}
	}
}

func TestDBServiceForType(t *testing.T) {
	cases := []struct {
		engine  string
		service string
		ok      bool
		hasNote bool
	}{
		{"mysql", "mysql", true, false},
		{"MariaDB", "mysql", true, true},
		{"postgres", "postgres", true, false},
		{"postgresql", "postgres", true, false},
		{"pgsql", "postgres", true, false},
		{"mssql", "", false, false},
	}
	for _, c := range cases {
		service, note, ok := dbServiceForType(c.engine)
		if service != c.service || ok != c.ok || (note != "") != c.hasNote {
			t.Errorf("dbServiceForType(%q) = (%q, note=%q, %v), want (%q, hasNote=%v, %v)",
				c.engine, service, note, ok, c.service, c.hasNote, c.ok)
		}
	}
}

// ── Herd ─────────────────────────────────────────────────────────────────────

func TestHerdSeed(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, "herd.yml", `name: herd-website
php: '8.3'
secured: true
aliases: ['herd-laravel']
services:
    mysql:
        version: 8.0.36
        port: '${DB_PORT}'
    redis:
        version: 7.0.0
        port: '${REDIS_PORT}'
    minio:
        version: latest
    soketi:
        version: 1.0
`)
	cfg, notes, ok := herdSeed(dir)
	if !ok {
		t.Fatal("herdSeed: ok=false, want true")
	}
	if cfg.PHPVersion != "8.3" {
		t.Errorf("PHPVersion = %q, want 8.3", cfg.PHPVersion)
	}
	if !cfg.Secured {
		t.Error("Secured = false, want true")
	}
	if got := strings.Join(cfg.Domains, ","); got != "herd-website,herd-laravel" {
		t.Errorf("Domains = %q, want herd-website,herd-laravel", got)
	}
	// Services are emitted in sorted Herd-key order: minio, mysql, redis,
	// soketi — so minio (→rustfs) lands first and soketi is skipped.
	if got := strings.Join(cfg.ServiceNames(), ","); got != "rustfs,mysql,redis" {
		t.Errorf("Services = %q, want rustfs,mysql,redis", got)
	}
	if !notesContain(notes, "8.0.36") || !notesContain(notes, "7.0.0") {
		t.Errorf("notes missing dropped versions: %v", notes)
	}
	if !notesContain(notes, "soketi") {
		t.Errorf("notes missing skipped service: %v", notes)
	}
}

func TestHerdSeedUnquotedPHP(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, "herd.yaml", "name: app\nphp: 8.4\n")
	cfg, _, ok := herdSeed(dir)
	if !ok || cfg.PHPVersion != "8.4" {
		t.Errorf("herdSeed unquoted php = (%q, ok=%v), want (8.4, true)", cfg.PHPVersion, ok)
	}
}

func TestHerdSeedMissing(t *testing.T) {
	if _, _, ok := herdSeed(t.TempDir()); ok {
		t.Error("herdSeed on empty dir: ok=true, want false")
	}
}

// ── DDEV ─────────────────────────────────────────────────────────────────────

func TestDdevSeedScalarDatabase(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, ".ddev/config.yaml", `name: my-project
type: laravel
php_version: "8.3"
nodejs_version: "20"
docroot: public
database: mariadb:11.8
additional_hostnames:
  - staging
additional_fqdns:
  - example.com
`)
	cfg, notes, ok := ddevSeed(dir)
	if !ok {
		t.Fatal("ddevSeed: ok=false, want true")
	}
	if cfg.PHPVersion != "8.3" || cfg.NodeVersion != "20" || cfg.PublicDir != "public" {
		t.Errorf("got php=%q node=%q docroot=%q", cfg.PHPVersion, cfg.NodeVersion, cfg.PublicDir)
	}
	if got := strings.Join(cfg.Domains, ","); got != "my-project,staging" {
		t.Errorf("Domains = %q, want my-project,staging", got)
	}
	if got := strings.Join(cfg.ServiceNames(), ","); got != "mysql" {
		t.Errorf("Services = %q, want mysql (mariadb folds into mysql)", got)
	}
	if !notesContain(notes, "MariaDB") || !notesContain(notes, "11.8") {
		t.Errorf("notes missing mariadb/version info: %v", notes)
	}
	if !notesContain(notes, "additional_fqdns") {
		t.Errorf("notes missing additional_fqdns warning: %v", notes)
	}
}

func TestDdevSeedNestedDatabase(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, ".ddev/config.yaml", `name: legacy
php_version: "8.2"
database:
  type: postgres
  version: "15"
`)
	cfg, _, ok := ddevSeed(dir)
	if !ok {
		t.Fatal("ddevSeed: ok=false, want true")
	}
	if got := strings.Join(cfg.ServiceNames(), ","); got != "postgres" {
		t.Errorf("Services = %q, want postgres", got)
	}
}

func TestDdevSeedNoDatabase(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, ".ddev/config.yaml", "name: minimal\nphp_version: \"8.3\"\n")
	cfg, _, ok := ddevSeed(dir)
	if !ok {
		t.Fatal("ddevSeed: ok=false, want true")
	}
	if len(cfg.Services) != 0 {
		t.Errorf("Services = %v, want none", cfg.ServiceNames())
	}
}

// ── Lando ────────────────────────────────────────────────────────────────────

func TestLandoSeed(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, ".lando.yml", `name: my-app
recipe: laravel
config:
  php: '8.2'
  webroot: public
  database: mariadb:10.11
proxy:
  appserver:
    - my-app.lndo.site
    - my-app.lndo.site:8080
services:
  cache:
    type: redis:6
  node:
    type: node:18
`)
	cfg, notes, ok := landoSeed(dir)
	if !ok {
		t.Fatal("landoSeed: ok=false, want true")
	}
	if cfg.PHPVersion != "8.2" || cfg.PublicDir != "public" {
		t.Errorf("got php=%q webroot=%q", cfg.PHPVersion, cfg.PublicDir)
	}
	if cfg.NodeVersion != "18" {
		t.Errorf("NodeVersion = %q, want 18 (from node service)", cfg.NodeVersion)
	}
	if got := strings.Join(cfg.Domains, ","); got != "my-app" {
		t.Errorf("Domains = %q, want my-app", got)
	}
	if got := strings.Join(cfg.ServiceNames(), ","); got != "mysql,redis" {
		t.Errorf("Services = %q, want mysql,redis", got)
	}
	if !notesContain(notes, "10.11") {
		t.Errorf("notes missing dropped database version: %v", notes)
	}
}

func TestLandoSeedMissing(t *testing.T) {
	if _, _, ok := landoSeed(t.TempDir()); ok {
		t.Error("landoSeed on empty dir: ok=true, want false")
	}
}

// Two Lando services of the same type must collapse to a single lerd service.
func TestLandoSeedDeduplicatesServices(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, ".lando.yml", `name: dup-app
config:
  php: '8.2'
services:
  cache:
    type: redis:6
  sessions:
    type: redis:7
`)
	cfg, _, ok := landoSeed(dir)
	if !ok {
		t.Fatal("landoSeed: ok=false, want true")
	}
	if got := strings.Join(cfg.ServiceNames(), ","); got != "redis" {
		t.Errorf("Services = %q, want a single deduplicated redis", got)
	}
}

// ── detectImportSeed ─────────────────────────────────────────────────────────

func TestDetectImportSeed(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		if _, ok := detectImportSeed(t.TempDir()); ok {
			t.Error("detectImportSeed on empty dir: ok=true, want false")
		}
	})

	t.Run("ddev", func(t *testing.T) {
		dir := t.TempDir()
		writeSeedFile(t, dir, ".ddev/config.yaml", "name: d\nphp_version: \"8.3\"\n")
		seed, ok := detectImportSeed(dir)
		if !ok || seed.label != ".ddev/config.yaml" {
			t.Errorf("detectImportSeed = (%q, %v), want (.ddev/config.yaml, true)", seed.label, ok)
		}
	})

	t.Run("herd wins over ddev", func(t *testing.T) {
		dir := t.TempDir()
		writeSeedFile(t, dir, "herd.yml", "name: h\nphp: '8.3'\n")
		writeSeedFile(t, dir, ".ddev/config.yaml", "name: d\nphp_version: \"8.2\"\n")
		seed, ok := detectImportSeed(dir)
		if !ok || seed.label != "herd.yml" {
			t.Errorf("detectImportSeed = (%q, %v), want (herd.yml, true)", seed.label, ok)
		}
	})
}

// Verify a seeded config is non-empty so applyImportSeed's IsEmpty guard
// behaves as expected.
func TestSeedIsNonEmpty(t *testing.T) {
	dir := t.TempDir()
	writeSeedFile(t, dir, "herd.yml", "name: app\nphp: '8.3'\n")
	cfg, _, ok := herdSeed(dir)
	if !ok {
		t.Fatal("herdSeed: ok=false")
	}
	if (&config.ProjectConfig{}).IsEmpty() != true {
		t.Error("empty ProjectConfig should report IsEmpty")
	}
	if cfg.IsEmpty() {
		t.Error("seeded ProjectConfig should not report IsEmpty")
	}
}

// TestSeedFileVariants exercises every supported filename for each tool, so a
// project using the .yml or .yaml spelling is detected and parsed either way.
func TestSeedFileVariants(t *testing.T) {
	cases := []struct {
		desc    string
		rel     string
		content string
		seed    func(string) (*config.ProjectConfig, []string, bool)
	}{
		{"herd.yml", "herd.yml", "name: a\nphp: '8.3'\n", herdSeed},
		{"herd.yaml", "herd.yaml", "name: a\nphp: '8.3'\n", herdSeed},
		{"ddev config.yaml", filepath.Join(".ddev", "config.yaml"), "name: a\nphp_version: \"8.3\"\n", ddevSeed},
		{"ddev config.yml", filepath.Join(".ddev", "config.yml"), "name: a\nphp_version: \"8.3\"\n", ddevSeed},
		{"lando .lando.yml", ".lando.yml", "name: a\nconfig:\n  php: '8.3'\n", landoSeed},
		{"lando .lando.yaml", ".lando.yaml", "name: a\nconfig:\n  php: '8.3'\n", landoSeed},
	}
	for _, c := range cases {
		dir := t.TempDir()
		writeSeedFile(t, dir, c.rel, c.content)

		cfg, _, ok := c.seed(dir)
		if !ok {
			t.Errorf("[%s] seed: ok=false, want true", c.desc)
			continue
		}
		if cfg.PHPVersion != "8.3" {
			t.Errorf("[%s] PHPVersion = %q, want 8.3", c.desc, cfg.PHPVersion)
		}
		seed, found := detectImportSeed(dir)
		if !found {
			t.Errorf("[%s] detectImportSeed: found=false, want true", c.desc)
			continue
		}
		if seed.label != c.rel {
			t.Errorf("[%s] label = %q, want %q (must name the actual file)", c.desc, seed.label, c.rel)
		}
	}
}
