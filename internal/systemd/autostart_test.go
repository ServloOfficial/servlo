package systemd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestAutostartUserUnits verifies that AutostartUserUnits returns the
// servlo-panel / servlo-watcher baseline plus every per-site
// servlo-*.service in the systemd/user/ directory, deduplicated and
// sorted, and that it ignores non-servlo units.
func TestAutostartUserUnits(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	systemdDir := filepath.Join(tmp, "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Per-site / per-worker units. servlo-panel is duplicated to verify dedup.
	for _, name := range []string{
		"servlo-panel.service",
		"servlo-watcher.service",
		"servlo-queue-myapp.service",
		"servlo-schedule-myapp.service",
		"servlo-horizon-myapp.service",
		"servlo-reverb-myapp.service",
		"servlo-stripe-myapp.service",
	} {
		if err := os.WriteFile(filepath.Join(systemdDir, name), []byte("[Service]\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-servlo unit that must be ignored.
	if err := os.WriteFile(filepath.Join(systemdDir, "other.service"), []byte("[Service]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got := AutostartUserUnits()
	want := []string{
		"servlo-horizon-myapp.service",
		"servlo-panel.service",
		"servlo-queue-myapp.service",
		"servlo-reverb-myapp.service",
		"servlo-schedule-myapp.service",
		"servlo-stripe-myapp.service",
		"servlo-watcher.service",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutostartUserUnits() mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

// TestAutostartUserUnitsBaseline verifies that servlo-panel and servlo-watcher
// are always included even when no files exist on disk.
// Without this, a fresh install (no per-site units yet) would have
// nothing to enable when the user toggles autostart back on.
func TestAutostartUserUnitsBaseline(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := AutostartUserUnits()
	want := []string{"servlo-panel.service", "servlo-watcher.service"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AutostartUserUnits() baseline mismatch\ngot:  %v\nwant: %v", got, want)
	}
}

// TestIsAutostartEnabledDefault verifies the safe default — when no
// config exists yet, autostart is treated as enabled, matching the
// historical behaviour of every install before this flag existed.
func TestIsAutostartEnabledDefault(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, "share"))

	if !IsAutostartEnabled() {
		t.Error("IsAutostartEnabled() should default to true when no config exists")
	}
}
