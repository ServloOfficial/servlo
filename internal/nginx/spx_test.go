package nginx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestGenerateVhost_ProfilerOnInjectsSpxEnabled(t *testing.T) {
	confD := setupConfD(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg, _ := config.LoadGlobal()
	cfg.Profiler.Enabled = true
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	site := config.Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if !strings.Contains(content, "SPX_KEY=$spx_key") {
		t.Errorf("expected SPX_KEY cookie injection in:\n%s", content)
	}
	if !strings.Contains(content, "SPX_ENABLED=1") {
		t.Errorf("profiler on should inject SPX_ENABLED=1 in:\n%s", content)
	}
}

func TestGenerateVhost_ProfilerOffOmitsSpxEnabled(t *testing.T) {
	confD := setupConfD(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	site := config.Site{Name: "myapp", Domains: []string{"myapp.test"}, Path: "/srv/myapp"}
	if err := GenerateVhost(site, "8.3"); err != nil {
		t.Fatalf("GenerateVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "myapp.test.conf"))
	if !strings.Contains(content, "SPX_KEY=$spx_key") {
		t.Errorf("expected SPX_KEY cookie injection even when off in:\n%s", content)
	}
	if strings.Contains(content, "SPX_ENABLED=1") {
		t.Errorf("profiler off must not inject SPX_ENABLED in:\n%s", content)
	}
}

func TestEnsureForwardedConf_WritesSpxKeyMap(t *testing.T) {
	confD := setupConfD(t)
	if err := EnsureForwardedConf(); err != nil {
		t.Fatalf("EnsureForwardedConf: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "_forwarded.conf"))
	if !strings.Contains(content, "map $http_x_forwarded_host $spx_key") {
		t.Errorf("expected $spx_key map in:\n%s", content)
	}
	key, err := config.LoadOrGenerateProfilerKey()
	if err != nil {
		t.Fatalf("LoadOrGenerateProfilerKey: %v", err)
	}
	if !strings.Contains(content, key) {
		t.Errorf("expected generated key %q in the map", key)
	}
}

func TestEnsureProfilerVhost_WritesVhost(t *testing.T) {
	confD := setupConfD(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := EnsureProfilerVhost(); err != nil {
		t.Fatalf("EnsureProfilerVhost: %v", err)
	}
	content := readConf(t, filepath.Join(confD, "_profiler.conf"))
	for _, want := range []string{"server_name profiler.localhost", "SPX_KEY=$spx_key", "fastcgi_pass"} {
		if !strings.Contains(content, want) {
			t.Errorf("profiler vhost missing %q in:\n%s", want, content)
		}
	}
}
