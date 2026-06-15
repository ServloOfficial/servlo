package cli

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geodro/lerd/internal/config"
)

// idleWorkerResumable must mirror resumeWorkerByName exactly: a worker the resume
// path can't bring back (an orphaned unit with no framework definition) must be
// reported non-resumable so idle-suspend never strands it stopped.
func TestIdleWorkerResumable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	dir := filepath.Join(tmp, "site")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lerd.yaml"), []byte("framework: laravel\n"), 0644); err != nil {
		t.Fatal(err)
	}
	proj, err := config.LoadProjectConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	proj.CustomWorkers = map[string]config.FrameworkWorker{
		"queue": {Command: "php artisan queue:work"},
	}
	proj.Proxy = &config.ProxyConfig{Command: "npm run dev", Port: 5173}
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}

	site := &config.Site{Name: "site", Framework: "laravel", Path: dir}
	cases := map[string]bool{
		"queue":             true,  // framework worker
		"stripe":            true,  // handled explicitly by resumeWorkerByName
		hostProxyWorkerName: true,  // resumable while the project declares a proxy
		"some-orphan-unit":  false, // no framework definition -> not resumable
	}
	for name, want := range cases {
		if got := idleWorkerResumable(site, name); got != want {
			t.Errorf("idleWorkerResumable(%q) = %v, want %v", name, got, want)
		}
	}

	// With the proxy block removed, the host-proxy worker is no longer resumable
	// (resumeWorkerByName would no-op), so idle-suspend must not stop it.
	proj.Proxy = nil
	if err := config.SaveProjectConfig(dir, proj); err != nil {
		t.Fatal(err)
	}
	if idleWorkerResumable(site, hostProxyWorkerName) {
		t.Error("host-proxy worker must be non-resumable when the project has no proxy block")
	}
}

func TestCompactDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{41 * time.Minute, "41m"},
		{90 * time.Minute, "1h"},
		{25 * time.Hour, "1d"},
		{72 * time.Hour, "3d"},
	}
	for _, tc := range cases {
		if got := compactDuration(tc.d); got != tc.want {
			t.Errorf("compactDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
