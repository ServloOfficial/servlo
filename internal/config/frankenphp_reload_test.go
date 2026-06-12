package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fwWithOctane builds a minimal Laravel-like framework with a FrankenPHP worker
// entrypoint and its watch variant, matching the shape of the built-in Laravel
// definition.
func fwWithOctane() *Framework {
	return &Framework{
		Name:      "laravel",
		PublicDir: "public",
		FrankenPHP: &FrameworkFrankenPHP{
			Entrypoint:       []string{"frankenphp", "php-server", "-l", ":8000", "-r", "public/"},
			WorkerEntrypoint: []string{"sh", "-c", "exec php artisan octane:start --server=frankenphp --workers=auto"},
			WorkerReloadEntrypoint: []string{"sh", "-c",
				"exec php artisan octane:start --server=frankenphp --workers=auto --watch"},
			SupportsWorker: true,
		},
	}
}

func TestResolveFrankenPHPWorkerEntrypoint(t *testing.T) {
	fw := fwWithOctane()

	t.Run("non-worker returns normal entrypoint", func(t *testing.T) {
		dir := t.TempDir()
		got := fw.ResolveFrankenPHPWorkerEntrypoint(dir, false)
		if strings.Join(got, " ") != strings.Join(fw.FrankenPHP.Entrypoint, " ") {
			t.Fatalf("expected normal entrypoint, got %v", got)
		}
	})

	t.Run("worker without reload opt-in keeps standard worker entrypoint", func(t *testing.T) {
		dir := t.TempDir()
		got := fw.ResolveFrankenPHPWorkerEntrypoint(dir, true)
		if strings.Join(got, " ") != strings.Join(fw.FrankenPHP.WorkerEntrypoint, " ") {
			t.Fatalf("expected standard worker entrypoint, got %v", got)
		}
	})

	t.Run("reload opt-in without chokidar falls back to standard", func(t *testing.T) {
		dir := t.TempDir()
		if err := SetProjectWorkerReload(dir, "octane", true); err != nil {
			t.Fatal(err)
		}
		got := fw.ResolveFrankenPHPWorkerEntrypoint(dir, true)
		if strings.Join(got, " ") != strings.Join(fw.FrankenPHP.WorkerEntrypoint, " ") {
			t.Fatalf("expected fallback to standard entrypoint when chokidar absent, got %v", got)
		}
	})

	t.Run("reload opt-in with chokidar selects watch variant", func(t *testing.T) {
		dir := t.TempDir()
		if err := SetProjectWorkerReload(dir, "octane", true); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "node_modules", "chokidar"), 0o755); err != nil {
			t.Fatal(err)
		}
		got := fw.ResolveFrankenPHPWorkerEntrypoint(dir, true)
		joined := strings.Join(got, " ")
		if !strings.Contains(joined, "octane:start") || !strings.Contains(joined, "--watch") {
			t.Fatalf("expected watch variant, got %v", got)
		}
		// --poll is appended only where the container can't see host fs events.
		if WatcherNeedsPolling(dir) {
			if !strings.HasSuffix(got[len(got)-1], "--poll") {
				t.Fatalf("expected --poll appended on polling host, got %v", got)
			}
		} else {
			if strings.Contains(joined, "--poll") {
				t.Fatalf("did not expect --poll on inotify host, got %v", got)
			}
		}
	})

	t.Run("no watch variant defined keeps standard even when opted in", func(t *testing.T) {
		dir := t.TempDir()
		if err := SetProjectWorkerReload(dir, "octane", true); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "node_modules", "chokidar"), 0o755); err != nil {
			t.Fatal(err)
		}
		bare := &Framework{
			Name:      "laravel",
			PublicDir: "public",
			FrankenPHP: &FrameworkFrankenPHP{
				WorkerEntrypoint: []string{"sh", "-c", "exec php artisan octane:start"},
				SupportsWorker:   true,
			},
		}
		got := bare.ResolveFrankenPHPWorkerEntrypoint(dir, true)
		if strings.Join(got, " ") != strings.Join(bare.FrankenPHP.WorkerEntrypoint, " ") {
			t.Fatalf("expected standard entrypoint when no watch variant, got %v", got)
		}
	})
}

func TestAppendPollFlag(t *testing.T) {
	t.Run("sh -c form appends inside the script", func(t *testing.T) {
		got := appendPollFlag([]string{"sh", "-c", "exec php artisan octane:start --watch"})
		want := "exec php artisan octane:start --watch --poll"
		if len(got) != 3 || got[2] != want {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("bare argv form appends a trailing arg", func(t *testing.T) {
		got := appendPollFlag([]string{"php", "artisan", "octane:start", "--watch"})
		if len(got) != 5 || got[4] != "--poll" {
			t.Fatalf("got %v", got)
		}
	})
}
