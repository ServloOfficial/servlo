package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// setupConfigHome points config.PHPConfFile / PHPUserIniFile / GlobalConfigFile
// at a temp directory by overriding XDG_CONFIG_HOME and XDG_DATA_HOME. Each test
// gets an isolated tree so concurrent test runs don't collide on the per-version
// ini paths.
//
// execCommand is also stubbed so the helpers under test (WriteContainerHosts
// calls DetectHostGatewayIP and nginxContainerIP, which shell out to podman,
// and DetectHostGatewayIP's last-resort fallback runs a real `podman run` of
// the alpine image when every other probe fails) can never reach a real
// binary. PATH alone doesn't guarantee that: PodmanBin() also tries hardcoded
// absolute install paths that bypass PATH entirely,
// and matching one on the host running the test would have podman actually
// pull and run a container, leaving container-storage overlay layers in the
// temp dir that the Go TempDir cleanup can't remove afterward.
func setupConfigHome(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("PATH", filepath.Join(tmp, "no-bin"))

	prev := execCommand
	t.Cleanup(func() { execCommand = prev })
	execCommand = fakeExec("", "", 1)
}

func TestEnsureUserIni_createsWhenMissing(t *testing.T) {
	setupConfigHome(t)
	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	info, err := os.Stat(config.PHPUserIniFile("8.4"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected regular file, got directory")
	}
}

func TestEnsureUserIni_noopWhenRegularFileExists(t *testing.T) {
	// User php.ini files are explicitly meant to be hand-edited (per the
	// header comment in the default content). EnsureUserIni must never
	// stomp the file once it's a regular file on disk.
	setupConfigHome(t)
	path := config.PHPUserIniFile("8.4")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("memory_limit = 1G\n")
	if err := os.WriteFile(path, custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(custom) {
		t.Errorf("user ini was rewritten:\ngot:  %s\nwant: %s", got, custom)
	}
}

// ── EnsureSharedIni (version-agnostic, same race surface) ────────────────────

func TestEnsureSharedIni_createsWhenMissing(t *testing.T) {
	setupConfigHome(t)
	if err := EnsureSharedIni(); err != nil {
		t.Fatalf("EnsureSharedIni: %v", err)
	}
	info, err := os.Stat(config.SharedIniFile())
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected regular file, got directory")
	}
}

func TestEnsureSharedIni_noopWhenRegularFileExists(t *testing.T) {
	setupConfigHome(t)
	path := config.SharedIniFile()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("memory_limit = 1G\n")
	if err := os.WriteFile(path, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSharedIni(); err != nil {
		t.Fatalf("EnsureSharedIni: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(custom) {
		t.Errorf("shared ini was rewritten:\ngot:  %s\nwant: %s", got, custom)
	}
}

func TestEnsureSharedIni_healsStaleDirectory(t *testing.T) {
	setupConfigHome(t)
	path := config.SharedIniFile()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSharedIni(); err != nil {
		t.Fatalf("EnsureSharedIni: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.IsDir() {
		t.Errorf("stale directory not healed into a file")
	}
}

// ── ensureFPMHostsFile (third bind-mount source on the FPM quadlet) ──────────

func TestEnsureFPMHostsFile_writesWhenMissing(t *testing.T) {
	// First-install case: the shared /etc/hosts hasn't been written yet
	// but the FPM container is about to start. Without pre-creation
	// podman would auto-create the path as a directory.
	setupConfigHome(t)
	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	info, err := os.Stat(config.ContainerHostsFile())
	if err != nil {
		t.Fatalf("hosts file not created: %v", err)
	}
	if info.IsDir() {
		t.Errorf("expected file, got directory")
	}
	body, _ := os.ReadFile(config.ContainerHostsFile())
	if !strings.Contains(string(body), "host.containers.internal") {
		t.Errorf("expected host.containers.internal in hosts file:\n%s", body)
	}
}

func TestEnsureFPMHostsFile_noopWhenRegularFileExists(t *testing.T) {
	// The file gets rewritten by WriteContainerHosts on every site
	// link/unlink/start, but the FPM-quadlet pre-create must NOT
	// rewrite it. Otherwise it would race with the watcher's reprobe
	// (which writes a verified host IP) and replace it with the
	// fallback header.
	setupConfigHome(t)
	hostsPath := config.ContainerHostsFile()
	if err := os.MkdirAll(filepath.Dir(hostsPath), 0755); err != nil {
		t.Fatal(err)
	}
	custom := []byte("# user-managed hosts file\n10.0.0.5 host.containers.internal\n")
	if err := os.WriteFile(hostsPath, custom, 0644); err != nil {
		t.Fatal(err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	got, _ := os.ReadFile(hostsPath)
	if string(got) != string(custom) {
		t.Errorf("hosts file was rewritten\ngot:  %s\nwant: %s", got, custom)
	}
}

func TestEnsureFPMHostsFile_fillsInServicePreCreatedFile(t *testing.T) {
	// EnsureServiceHostsFile writes a minimal file so a service quadlet's mount
	// source exists, and it deliberately stays podman-free, so it has no host
	// gateway line. The FPM path must not mistake that for a finished file.
	setupConfigHome(t)
	hostsPath := config.ContainerHostsFile()
	if err := EnsureServiceHostsFile(false); err != nil {
		t.Fatalf("EnsureServiceHostsFile: %v", err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	body, _ := os.ReadFile(hostsPath)
	if !strings.Contains(string(body), "host.containers.internal") {
		t.Errorf("expected the gateway entry to be filled in:\n%s", body)
	}
}

func TestEnsureFPMHostsFile_healsStaleDirectory(t *testing.T) {
	// A podman-auto-created directory sits at the bind-mount source.
	// ensureFPMHostsFile must remove it before writing the real file or
	// the next FPM start fails the same way.
	setupConfigHome(t)
	hostsPath := config.ContainerHostsFile()
	if err := os.MkdirAll(hostsPath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ensureFPMHostsFile(); err != nil {
		t.Fatalf("ensureFPMHostsFile: %v", err)
	}
	info, err := os.Stat(hostsPath)
	if err != nil {
		t.Fatalf("path missing after heal: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path is still a directory, heal failed")
	}
}

func TestEnsureUserIni_healsStaleDirectory(t *testing.T) {
	// Same race, on the user-ini bind-mount
	// declared in servlo-php-fpm.container.tmpl. EnsureUserIni currently
	// only checks whether the path exists, not whether it's a regular
	// file, so a podman-auto-created directory passes the check and the
	// real ini is never written. This test fails until EnsureUserIni
	// learns to heal a stale directory.
	setupConfigHome(t)
	path := config.PHPUserIniFile("8.4")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	if err := EnsureUserIni("8.4"); err != nil {
		t.Fatalf("EnsureUserIni: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("path missing after heal: %v", err)
	}
	if info.IsDir() {
		t.Errorf("path is still a directory, heal failed")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "PHP "+"8.4") {
		t.Errorf("expected default user ini content, got:\n%s", body)
	}
}
