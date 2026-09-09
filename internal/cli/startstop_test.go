package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A host-proxy site whose .servlo.yaml dev command has drifted from the approved
// one must still be enumerated, because this list also drives servlo stop/quit:
// excluding the unit would leave a running dev server unstoppable.
func TestRegisteredFrameworkWorkerUnits_EnumeratesDriftedHostProxy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	if err := config.SaveProjectConfig(dir, &config.ProjectConfig{
		Proxy: &config.ProxyConfig{Command: "npm run drifted", Port: 5173},
	}); err != nil {
		t.Fatal(err)
	}
	if err := config.AddSite(config.Site{
		Name: "site", Domains: []string{"site.test"}, Path: dir,
		HostPort: 5173, HostCommand: "npm run approved",
	}); err != nil {
		t.Fatal(err)
	}
	want := config.HostProxyWorkerUnit("site")
	found := false
	for _, u := range registeredFrameworkWorkerUnits() {
		if u == want {
			found = true
		}
	}
	if !found {
		t.Errorf("drifted host-proxy unit %q must stay enumerated so stop/quit can stop it", want)
	}
}

func TestQuadletImage_found(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "servlo-nginx.container"), []byte("[Container]\nImage=docker.io/library/nginx:alpine\n"), 0644)

	got := quadletImage("servlo-nginx")
	if got != "docker.io/library/nginx:alpine" {
		t.Errorf("quadletImage = %q, want docker.io/library/nginx:alpine", got)
	}
}

func TestQuadletImage_missing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	got := quadletImage("servlo-nonexistent")
	if got != "" {
		t.Errorf("quadletImage = %q, want empty for missing unit", got)
	}
}

func TestQuadletImage_noImageLine(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	dir := filepath.Join(tmp, "containers", "systemd")
	os.MkdirAll(dir, 0755)
	os.WriteFile(filepath.Join(dir, "servlo-test.container"), []byte("[Container]\nContainerName=test\n"), 0644)

	got := quadletImage("servlo-test")
	if got != "" {
		t.Errorf("quadletImage = %q, want empty when no Image= line", got)
	}
}

func TestIsPortConflict(t *testing.T) {
	const portList = "someapp 60498 sdp 5u IPv4 TCP 127.0.0.1:5300 (LISTEN)"
	mariadb := PortCheck{Port: "3306", Label: "mariadb", Container: "servlo-mariadb"}

	running := func(string) bool { return true }
	notRunning := func(string) bool { return false }

	tests := []struct {
		name             string
		check            PortCheck
		ports            string
		containerRunning func(string) bool
		want             bool
	}{
		// A running container owns its port directly — never a conflict.
		{"running container owns its port", mariadb, "mysqld 1 sdp 3u TCP 127.0.0.1:3306 (LISTEN)", running, false},
		// Non-dns service, not running, foreign listener — real conflict.
		{"foreign process holds a service port", mariadb, "someapp 999 sdp 3u TCP 127.0.0.1:3306 (LISTEN)", notRunning, true},
		// Port is free — no conflict regardless.
		{"port free", mariadb, portList, notRunning, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPortConflict(tt.check, tt.ports, tt.containerRunning)
			if got != tt.want {
				t.Errorf("isPortConflict() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestQuitProcessUnits_FullTeardown pins that `servlo quit` takes down the host
// process units `servlo stop` leaves running.
//
// It used to pin an ordering too: the DNS unit had to be stopped after the
// watcher, because the watcher was the only thing that would restart it. S2.1
// deleted that unit, and with it the one unit here whose position mattered —
// so what is left to assert is the set, and that it no longer names a unit
// nothing writes.
func TestQuitProcessUnits_FullTeardown(t *testing.T) {
	units := quitProcessUnits()
	want := []string{"servlo-panel", "servlo-watcher"}
	if !slices.Equal(units, want) {
		t.Errorf("quitProcessUnits() = %v, want %v", units, want)
	}
}
