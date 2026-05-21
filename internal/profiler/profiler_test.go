package profiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func tempXDG(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
}

func TestSetProfiling_GlobalToggle(t *testing.T) {
	tempXDG(t)
	old := nginxReloadFn
	nginxReloadFn = func() error { return nil }
	defer func() { nginxReloadFn = old }()

	if err := config.AddSite(config.Site{
		Name: "myapp", Domains: []string{"myapp.test"},
		Path: "/srv/myapp", PHPVersion: "8.3",
	}); err != nil {
		t.Fatalf("AddSite: %v", err)
	}

	res, err := SetProfiling(true)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !res.Enabled || res.NoChange {
		t.Errorf("expected enabled, got %+v", res)
	}
	if cfg, _ := config.LoadGlobal(); cfg == nil || !cfg.IsProfilerEnabled() {
		t.Errorf("profiler flag not persisted")
	}
	conf, err := os.ReadFile(filepath.Join(config.NginxConfD(), "myapp.test.conf"))
	if err != nil {
		t.Fatalf("read vhost: %v", err)
	}
	if !strings.Contains(string(conf), "SPX_ENABLED=1") {
		t.Errorf("vhost missing SPX_ENABLED after enable:\n%s", conf)
	}

	if res2, _ := SetProfiling(true); !res2.NoChange {
		t.Errorf("second enable should report no-change")
	}

	if _, err := SetProfiling(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	conf2, _ := os.ReadFile(filepath.Join(config.NginxConfD(), "myapp.test.conf"))
	if strings.Contains(string(conf2), "SPX_ENABLED=1") {
		t.Errorf("vhost still injects SPX_ENABLED after disable:\n%s", conf2)
	}
}
