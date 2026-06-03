package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// isolateGlobalConfig points the global config (and XDG roots) at a temp dir so
// SaveGlobal/LoadGlobal never touch the developer's real machine.
func isolateGlobalConfig(t *testing.T, horizonReload bool) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	cfg, err := config.LoadGlobal()
	if err != nil {
		t.Fatalf("LoadGlobal: %v", err)
	}
	cfg.SetHorizonReload(horizonReload)
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
}

// siteWithChokidar returns a temp site dir; when withChokidar is true it seeds
// node_modules/chokidar so the watcher prerequisite is satisfied.
func siteWithChokidar(t *testing.T, withChokidar bool) string {
	t.Helper()
	site := t.TempDir()
	if withChokidar {
		if err := os.MkdirAll(filepath.Join(site, "node_modules", "chokidar"), 0o755); err != nil {
			t.Fatalf("seed chokidar: %v", err)
		}
	}
	return site
}

func TestResolveHorizonCommand(t *testing.T) {
	const base = "php artisan horizon"

	t.Run("reload off keeps the standard command", func(t *testing.T) {
		isolateGlobalConfig(t, false)
		site := siteWithChokidar(t, true)
		if got := resolveHorizonCommand("horizon", site, base); got != base {
			t.Errorf("got %q, want %q", got, base)
		}
	})

	t.Run("reload on with chokidar swaps to horizon:listen --poll", func(t *testing.T) {
		isolateGlobalConfig(t, true)
		site := siteWithChokidar(t, true)
		want := "php artisan horizon:listen --poll"
		if got := resolveHorizonCommand("horizon", site, base); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("reload on without chokidar falls back to the standard command", func(t *testing.T) {
		isolateGlobalConfig(t, true)
		site := siteWithChokidar(t, false)
		if got := resolveHorizonCommand("horizon", site, base); got != base {
			t.Errorf("got %q, want %q (should fall back when chokidar missing)", got, base)
		}
	})

	t.Run("non-horizon workers are never rewritten", func(t *testing.T) {
		isolateGlobalConfig(t, true)
		site := siteWithChokidar(t, true)
		const queue = "php artisan queue:work --queue=default --tries=3 --timeout=60"
		if got := resolveHorizonCommand("queue", site, queue); got != queue {
			t.Errorf("got %q, want %q", got, queue)
		}
	})

	t.Run("derives from the base so extra flags are preserved", func(t *testing.T) {
		isolateGlobalConfig(t, true)
		site := siteWithChokidar(t, true)
		got := resolveHorizonCommand("horizon", site, "php artisan horizon --environment=local")
		want := "php artisan horizon:listen --environment=local --poll"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})
}
