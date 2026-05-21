package podman

import (
	"os"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

func TestSpxIni_SubstitutesKey(t *testing.T) {
	withTempXDG(t)

	ini, err := SpxIni()
	if err != nil {
		t.Fatalf("SpxIni: %v", err)
	}
	if strings.Contains(ini, "{{ SPX_KEY }}") {
		t.Errorf("SPX_KEY placeholder not substituted: %s", ini)
	}
	if !strings.Contains(ini, "spx.http_enabled=1") {
		t.Errorf("ini missing http_enabled: %s", ini)
	}
	if !strings.Contains(ini, "spx.data_dir=/var/spx") {
		t.Errorf("ini missing data_dir: %s", ini)
	}
	key, _ := config.LoadOrGenerateProfilerKey()
	if !strings.Contains(ini, "spx.http_key="+key) {
		t.Errorf("ini missing the generated key %q: %s", key, ini)
	}
}

func TestEnsureProfilerAssets_WritesIniAndDataDir(t *testing.T) {
	withTempXDG(t)

	if err := EnsureProfilerAssets(); err != nil {
		t.Fatalf("EnsureProfilerAssets: %v", err)
	}
	ini, err := os.ReadFile(config.SpxIniFile())
	if err != nil {
		t.Fatalf("read spx ini: %v", err)
	}
	if !strings.Contains(string(ini), "spx.http_key=") {
		t.Errorf("written ini missing key")
	}
	if info, err := os.Stat(config.SpxDataDir()); err != nil || !info.IsDir() {
		t.Errorf("spx data dir not created: %v", err)
	}
}

func TestEnsureProfilerAssets_ReplacesStaleDir(t *testing.T) {
	withTempXDG(t)

	// Simulate podman auto-creating a directory at the ini bind-mount source.
	if err := os.MkdirAll(config.SpxIniFile(), 0755); err != nil {
		t.Fatalf("mkdir stale: %v", err)
	}
	if err := EnsureProfilerAssets(); err != nil {
		t.Fatalf("EnsureProfilerAssets: %v", err)
	}
	info, err := os.Stat(config.SpxIniFile())
	if err != nil || info.IsDir() {
		t.Errorf("stale dir not replaced with a file: %v", err)
	}
}
