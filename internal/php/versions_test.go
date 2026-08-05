package php

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/realrashid/servlo/internal/podman"
)

// --- fpmQuadletRe ---

func TestFpmQuadletRe_matchesValidNames(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		matches bool
	}{
		{"servlo-php84-fpm.container", "8.4", true},
		{"servlo-php83-fpm.container", "8.3", true},
		{"servlo-php74-fpm.container", "7.4", true},
		{"servlo-nginx.container", "", false},
		{"servlo-php-fpm.container", "", false},
		{"servlo-php8-fpm.container", "", false},
	}
	for _, tt := range tests {
		sub := fpmQuadletRe.FindStringSubmatch(tt.name)
		if tt.matches {
			if len(sub) != 3 {
				t.Errorf("%s: expected match, got %v", tt.name, sub)
				continue
			}
			got := sub[1] + "." + sub[2]
			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
			}
		} else {
			if len(sub) == 3 {
				t.Errorf("%s: expected no match, got %v", tt.name, sub)
			}
		}
	}
}

// --- quadletExists ---

func TestQuadletExists_found(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "servlo-php84-fpm.container"), []byte("[Container]\n"), 0644)

	if !quadletExists("8.4") {
		t.Error("expected quadletExists to return true")
	}
}

func TestQuadletExists_missing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", tmp)

	if quadletExists("8.4") {
		t.Error("expected quadletExists to return false for missing quadlet")
	}
}

// --- ListInstalled (quadlet source) ---

func TestListInstalled_fromQuadlets(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "servlo-php84-fpm.container"), []byte("[Container]\n"), 0644)
	os.WriteFile(filepath.Join(dir, "servlo-php83-fpm.container"), []byte("[Container]\n"), 0644)

	versions, err := ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	found := map[string]bool{}
	for _, v := range versions {
		found[v] = true
	}
	if !found["8.4"] {
		t.Error("expected 8.4 in installed versions")
	}
	if !found["8.3"] {
		t.Error("expected 8.3 in installed versions")
	}
}

// ListInstalled used to call WriteFPMQuadlet -> DaemonReloadFn whenever it
// saw a running FPM container without a matching local quadlet, which under
// `go test` daemon-reloaded the user's real systemd and stopped every
// worker via BindsTo. Pin the read-only contract here so it can't regress.
func TestListInstalled_hasNoSideEffects(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only quadlet path")
	}

	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	prev := podman.DaemonReloadFn
	t.Cleanup(func() { podman.DaemonReloadFn = prev })
	var reloads int
	podman.DaemonReloadFn = func() error { reloads++; return nil }

	if _, err := ListInstalled(); err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}

	if reloads != 0 {
		t.Errorf("ListInstalled triggered %d daemon-reloads, want 0", reloads)
	}

	systemd := filepath.Join(tmp, "containers", "systemd")
	if entries, err := os.ReadDir(systemd); err == nil && len(entries) > 0 {
		t.Errorf("ListInstalled wrote %d entries under %s, want 0", len(entries), systemd)
	}
}
