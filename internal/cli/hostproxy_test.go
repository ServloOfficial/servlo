package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/geodro/lerd/internal/config"
	"github.com/geodro/lerd/internal/siteops"
)

// TestStopSiteWorkersHookRegistered guards the wiring that makes every unlink
// path (CLI, MCP, parked-watcher) stop a site's workers. Without it the
// host-proxy dev server leaks on the MCP and watcher paths.
func TestStopSiteWorkersHookRegistered(t *testing.T) {
	if siteops.StopSiteWorkers == nil {
		t.Fatal("cli init must register siteops.StopSiteWorkers")
	}
}

func TestHostProxyWorkerUnit_sharedWithConfig(t *testing.T) {
	if hostProxyWorkerUnit("myapp") != config.HostProxyWorkerUnit("myapp") {
		t.Error("cli and config must agree on the host-proxy worker unit name")
	}
}

func writePkgJSON(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestAvailableDevScripts_priorityOrder(t *testing.T) {
	dir := t.TempDir()
	writePkgJSON(t, dir, `{"scripts":{"start":"node x","dev":"vite","start:dev":"nest start --watch","build":"vite build"}}`)
	got := AvailableDevScripts(dir)
	want := []string{"npm run start:dev", "npm run dev", "npm run start"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestAvailableDevScripts_noPackageJSON(t *testing.T) {
	if got := AvailableDevScripts(t.TempDir()); got != nil {
		t.Errorf("expected nil for missing package.json, got %v", got)
	}
}

func TestPortFromCommand(t *testing.T) {
	cases := map[string]int{
		"vite --port 4000":       4000,
		"ng serve --port=4300":   4300,
		"PORT=8080 node main.js": 8080,
		"npm run dev":            0,
	}
	for cmd, want := range cases {
		if got := portFromCommand(cmd); got != want {
			t.Errorf("portFromCommand(%q) = %d, want %d", cmd, got, want)
		}
	}
}

func TestBuildProjectServices_builtins(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	got := buildProjectServices([]string{"redis", "mysql"}, &config.ProjectConfig{})
	if len(got) != 2 {
		t.Fatalf("expected 2 services, got %d: %+v", len(got), got)
	}
	if got[0].Name != "redis" || got[0].Preset != "" || got[0].Custom != nil {
		t.Errorf("redis built-in mapped wrong: %+v", got[0])
	}
	if got[1].Name != "mysql" {
		t.Errorf("mysql mapped wrong: %+v", got[1])
	}
}

func TestBuildHostProxyCommand_injectsPort(t *testing.T) {
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "npm run start:dev", Port: 3000})
	want := "env PORT=3000 npm run start:dev"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_customEnvKey(t *testing.T) {
	got := buildHostProxyCommand(&config.ProxyConfig{Command: "node server.js", Port: 4000, PortEnvKey: "APP_PORT"})
	want := "env APP_PORT=4000 node server.js"
	if got != want {
		t.Errorf("buildHostProxyCommand = %q, want %q", got, want)
	}
}

func TestBuildHostProxyCommand_proxyOnlyMode(t *testing.T) {
	// No command means proxy-only: lerd supervises nothing.
	if got := buildHostProxyCommand(&config.ProxyConfig{Port: 3000}); got != "" {
		t.Errorf("buildHostProxyCommand with no command = %q, want empty", got)
	}
}

func TestFirstFreePort(t *testing.T) {
	// 3000 and 3001 taken → first free is 3002.
	taken := map[int]bool{3000: true, 3001: true}
	if got := firstFreePort(3000, func(p int) bool { return taken[p] }); got != 3002 {
		t.Errorf("firstFreePort = %d, want 3002", got)
	}
	// Nothing taken → start is returned unchanged.
	if got := firstFreePort(5173, func(int) bool { return false }); got != 5173 {
		t.Errorf("firstFreePort = %d, want 5173 (start, none taken)", got)
	}
	// Below-range start is clamped to 1.
	if got := firstFreePort(0, func(int) bool { return false }); got != 1 {
		t.Errorf("firstFreePort(0) = %d, want 1", got)
	}
}
