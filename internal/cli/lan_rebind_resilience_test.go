package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/services"
)

// rebindMgr reports every unit active and fails the restart of one of them, the
// way a container with a broken image or an occupied port does.
type rebindMgr struct {
	services.ServiceManager
	failing   string
	restarted []string
}

func (m *rebindMgr) DaemonReload() error { return nil }

func (m *rebindMgr) UnitStatus(string) (string, error) { return "active", nil }

func (m *rebindMgr) Restart(name string) error {
	m.restarted = append(m.restarted, name)
	if name == m.failing {
		return fmt.Errorf("unit %s failed to start", name)
	}
	return nil
}

func writeServiceQuadlet(t *testing.T, name, ports string) {
	t.Helper()
	dir := config.QuadletDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := podman.CustomServiceQuadletMarker + "\n[Container]\nImage=docker.io/library/redis:7\nNetwork=servlo\n" + ports + "\n"
	if err := os.WriteFile(filepath.Join(dir, name+".container"), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// One container failing to restart must not strand the containers after it on
// their old LAN bind while every status surface claims loopback-only.
func TestRegenerateLANQuadletsRestartsEveryUnitDespiteFailure(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.GlobalConfig{}
	cfg.LAN.Exposed = true
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	// Alphabetically first fails, so a loop that aborts never reaches the rest.
	writeServiceQuadlet(t, "servlo-aaa", "PublishPort=[::]:1111:1111")
	writeServiceQuadlet(t, "servlo-mmm", "PublishPort=[::]:2222:2222")
	writeServiceQuadlet(t, "servlo-zzz", "PublishPort=[::]:3333:3333")

	mgr := &rebindMgr{failing: "servlo-aaa"}
	prev := services.Mgr
	services.Mgr = mgr
	t.Cleanup(func() { services.Mgr = prev })

	err := regenerateLANContainerQuadlets(nil)
	if err == nil {
		t.Fatal("a failed restart must be reported, not swallowed")
	}
	if !strings.Contains(err.Error(), "servlo-aaa") {
		t.Errorf("error %q does not name the unit that failed", err)
	}
	for _, name := range []string{"servlo-mmm", "servlo-zzz"} {
		if !slices.Contains(mgr.restarted, name) {
			t.Errorf("%s was never restarted; restarted = %v", name, mgr.restarted)
		}
	}
}

// With the files already correct, the runtime probe is the only thing that can
// notice a container still bound to the LAN, and it must drive a restart.
func TestRegenerateLANQuadletsHealsRuntimeDrift(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &config.GlobalConfig{}
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}
	writeServiceQuadlet(t, "servlo-redis", "PublishPort=127.0.0.1:6379:6379")

	prevProbe := podman.ContainerPublishesLANFn
	podman.ContainerPublishesLANFn = func(name string) (bool, bool) { return name == "servlo-redis", true }
	t.Cleanup(func() { podman.ContainerPublishesLANFn = prevProbe })

	mgr := &rebindMgr{}
	prev := services.Mgr
	services.Mgr = mgr
	t.Cleanup(func() { services.Mgr = prev })

	if err := regenerateLANContainerQuadlets(nil); err != nil {
		t.Fatalf("regenerateLANContainerQuadlets: %v", err)
	}
	if !slices.Contains(mgr.restarted, "servlo-redis") {
		t.Fatalf("stranded container was not restarted; restarted = %v", mgr.restarted)
	}
}
