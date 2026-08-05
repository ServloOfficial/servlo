package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// installFakeMkcert writes a stub mkcert binary into the test's bin dir.
// The stub echoes every SAN it was asked for into the cert file body so
// callers can grep for them. Mirrors the pattern used by the certs
// package tests so it stays familiar.
func installFakeMkcert(t *testing.T, dataHome string) {
	t.Helper()
	binDir := filepath.Join(dataHome, "servlo", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const script = `#!/bin/sh
CRT=""
KEY=""
SANS=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -cert-file) shift; CRT="$1" ;;
    -key-file)  shift; KEY="$1" ;;
    *) SANS="$SANS $1" ;;
  esac
  shift
done
printf '%s' "$SANS" > "$CRT"
printf 'KEY' > "$KEY"
`
	if err := os.WriteFile(filepath.Join(binDir, "mkcert"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// stubPodmanOnPath installs a no-op podman binary into stubDir and prepends
// stubDir to PATH. Without it nginx.Reload would shell out to real podman
// and create container storage under the test's tmp XDG_DATA_HOME that
// the runner can't delete (permission denied on overlay diffs).
func stubPodmanOnPath(t *testing.T) {
	t.Helper()
	stubDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stubDir, "podman"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// setupSecuredSite primes XDG, fake mkcert, global config, and registers a
// secured site at sitePath with the given domains. Returns the site struct
// so the test can call config.AddSite again after mutating Domains, etc.
func setupSecuredSite(t *testing.T, primary string, extras ...string) (sitePath string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	stubPodmanOnPath(t)
	installFakeMkcert(t, tmp)

	cfg := &config.GlobalConfig{}
	cfg.DNS.Enabled = true
	cfg.DNS.TLD = "test"
	if err := config.SaveGlobal(cfg); err != nil {
		t.Fatalf("SaveGlobal: %v", err)
	}

	sitePath = filepath.Join(tmp, "project")
	if err := os.MkdirAll(sitePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sitePath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	domains := append([]string{primary + ".test"}, extras...)
	site := config.Site{
		Name:    strings.TrimSuffix(primary, ".test"),
		Path:    sitePath,
		Domains: domains,
		Secured: true,
	}
	if err := config.AddSite(site); err != nil {
		t.Fatalf("AddSite: %v", err)
	}
	return sitePath
}

// readSiteCert returns the raw bytes of <XDG_DATA_HOME>/servlo/certs/sites/<primary>.crt.
func readSiteCert(t *testing.T, primary string) string {
	t.Helper()
	path := filepath.Join(os.Getenv("XDG_DATA_HOME"), "servlo", "certs", "sites", primary+".crt")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading cert %q: %v", path, err)
	}
	return string(body)
}
