package podman

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

func TestRebindInstalledQuadletsForLANKeepsServicesPrivateByDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeLANConfig(t, true)
	writeLANQuadlet(t, "servlo-nginx", false, "PublishPort=127.0.0.1:443:443\nPublishPort=[::1]:443:443")
	writeLANQuadlet(t, "servlo-redis", true, "PublishPort=127.0.0.1:6379:6379\nPublishPort=[::1]:6379:6379")

	changed, err := RebindInstalledQuadletsForLAN()
	if err != nil {
		t.Fatalf("RebindInstalledQuadletsForLAN: %v", err)
	}
	if !slices.Equal(changed, []string{"servlo-nginx"}) {
		t.Fatalf("changed units = %v, want [servlo-nginx]", changed)
	}
	if content := readLANQuadlet(t, "servlo-nginx"); strings.Contains(content, "127.0.0.1:") || strings.Contains(content, "[::1]:") {
		t.Fatalf("nginx remains loopback-bound:\n%s", content)
	}
	if content := readLANQuadlet(t, "servlo-redis"); !strings.Contains(content, "PublishPort=127.0.0.1:6379:6379") {
		t.Fatalf("redis did not remain loopback-bound:\n%s", content)
	}
}

// Services stay on loopback even with the services-exposed setting on. That
// setting predates the design law in CLAUDE.md 3.7, and a database reachable
// from off the machine is a database anyone who finds the port can attack. Only
// nginx has a reason to bind beyond loopback, because only nginx serves the
// sites.
func TestRebindInstalledQuadletsForLANNeverExposesAService(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeLANConfig(t, true)
	writeLANQuadlet(t, "servlo-nginx", false, "PublishPort=127.0.0.1:443:443\nPublishPort=[::1]:443:443")
	writeLANQuadlet(t, "servlo-mysql", true, "PublishPort=127.0.0.1:3306:3306\nPublishPort=[::1]:3306:3306")
	writeLANQuadlet(t, "servlo-custom-search", true, "PublishPort=127.0.0.1:7700:7700\nPublishPort=[::1]:7700:7700")
	writeLANQuadlet(t, "servlo-site-worker", false, "PublishPort=127.0.0.1:9000:9000\nPublishPort=[::1]:9000:9000")

	changed, err := RebindInstalledQuadletsForLAN()
	if err != nil {
		t.Fatalf("RebindInstalledQuadletsForLAN: %v", err)
	}
	if !slices.Equal(changed, []string{"servlo-nginx"}) {
		t.Fatalf("changed units = %v, want only nginx", changed)
	}
	if content := readLANQuadlet(t, "servlo-nginx"); strings.Contains(content, "127.0.0.1:") {
		t.Errorf("nginx remains loopback-bound:\n%s", content)
	}
	for _, name := range []string{"servlo-mysql", "servlo-custom-search", "servlo-site-worker"} {
		content := readLANQuadlet(t, name)
		if !strings.Contains(content, "PublishPort=127.0.0.1:") {
			t.Errorf("%s was exposed beyond loopback:\n%s", name, content)
		}
	}
}

func TestRebindInstalledQuadletsForLANRestoresLoopbackAndIsIdempotent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeLANConfig(t, false)
	writeLANQuadlet(t, "servlo-redis", true, "PublishPort=[::]:6379:6379")

	changed, err := RebindInstalledQuadletsForLAN()
	if err != nil {
		t.Fatalf("RebindInstalledQuadletsForLAN: %v", err)
	}
	if !slices.Equal(changed, []string{"servlo-redis"}) {
		t.Fatalf("changed units = %v, want [servlo-redis]", changed)
	}
	content := readLANQuadlet(t, "servlo-redis")
	if !strings.Contains(content, "PublishPort=127.0.0.1:6379:6379") || !strings.Contains(content, "PublishPort=[::1]:6379:6379") {
		t.Fatalf("redis was not restored to dual-stack loopback:\n%s", content)
	}

	changed, err = RebindInstalledQuadletsForLAN()
	if err != nil {
		t.Fatalf("second RebindInstalledQuadletsForLAN: %v", err)
	}
	if len(changed) != 0 {
		t.Fatalf("idempotent rebind changed %v", changed)
	}
}

// A service installed while the LAN settings are on comes up loopback-bound
// like every other one. The write path and the rebind path have to agree, or a
// service added after the setting was flipped would be the one exposed.
func TestWriteQuadletDiffKeepsANewServiceOnLoopback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeLANConfig(t, true)
	content := CustomServiceQuadletMarker + "\n[Container]\nImage=docker.io/library/redis:7.4.9-alpine\nNetwork=servlo\nPublishPort=6379:6379\n"

	if _, err := WriteQuadletDiff("servlo-redis", content); err != nil {
		t.Fatalf("WriteQuadletDiff: %v", err)
	}
	written := readLANQuadlet(t, "servlo-redis")
	if !strings.Contains(written, "PublishPort=127.0.0.1:6379:6379") {
		t.Fatalf("a new service was published beyond loopback:\n%s", written)
	}
}

func writeLANConfig(t *testing.T, exposed bool) {
	t.Helper()
	cfg := &config.GlobalConfig{}
	cfg.LAN.Exposed = exposed
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
}

func writeLANQuadlet(t *testing.T, name string, managedService bool, ports string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir quadlet dir: %v", err)
	}
	marker := ""
	if managedService {
		marker = CustomServiceQuadletMarker + "\n"
	}
	content := marker + "[Container]\nImage=docker.io/library/redis:7.4.9-alpine\nNetwork=servlo\n" + ports + "\n\n[Service]\nRestart=always\n\n[Install]\nWantedBy=default.target\n"
	if err := os.WriteFile(filepath.Join(dir, name+".container"), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func readLANQuadlet(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(config.QuadletDir(), name+".container"))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(content)
}
