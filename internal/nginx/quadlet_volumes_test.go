package nginx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/podman"
)

// Every host path the nginx container mounts has to exist before it starts.
//
// Podman refuses a bind mount whose source is not there, with a statfs error
// and exit 125, and systemd's restart loop cannot fix a missing directory. So
// the container never comes up, every site stops serving, and the message
// names a path rather than the feature that added it. That is exactly what a
// mount added for staging's htpasswd files did: it was written into the quadlet
// and nothing created the directory, so a fresh install had no nginx at all.
//
// Derived from the quadlet rather than from a list, because a list beside the
// template is a list that goes stale the next time a mount is added.
func TestEnsureNginxConfig_createsEveryMountTheQuadletDeclares(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("HOME", filepath.Join(sandbox, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, "data"))

	if err := EnsureNginxConfig(); err != nil {
		t.Fatalf("EnsureNginxConfig: %v", err)
	}

	tmpl, err := podman.GetQuadletTemplate("servlo-nginx.container")
	if err != nil {
		t.Fatal(err)
	}
	const dataPrefix = "%h/.local/share/servlo/"
	checked := 0
	for _, line := range strings.Split(string(tmpl), "\n") {
		if !strings.HasPrefix(line, "Volume=") {
			continue
		}
		src, _, ok := strings.Cut(strings.TrimPrefix(line, "Volume="), ":")
		if !ok || !strings.HasPrefix(src, dataPrefix) {
			continue
		}
		checked++
		path := filepath.Join(config.DataDir(), filepath.FromSlash(strings.TrimPrefix(src, dataPrefix)))
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the nginx quadlet mounts %s and nothing creates it, so podman refuses to start the container: %v", src, err)
		}
	}
	if checked == 0 {
		t.Fatal("no mounts were checked, so this proves nothing")
	}
}

// RewriteNginxQuadlet must preserve the Volume= lines for paths outside $HOME.
// It renders the bundled template, which carries no site mounts, so without
// re-injecting them a site parked outside home loses its bind mount from nginx
// and its docroot becomes unreadable inside the container.
func TestRewriteNginxQuadlet_keepsExtraVolumes(t *testing.T) {
	sandbox := t.TempDir()
	home := filepath.Join(sandbox, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, "data"))

	cfgDir := filepath.Join(sandbox, "config", "servlo")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A project parked outside $HOME: the case ExtraVolumePaths exists for.
	// The directory has to exist, missing sources are dropped so podman never
	// meets a bind mount it would refuse to start (#1083).
	outside := filepath.Join(sandbox, "apps")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "parked_directories:\n    - " + outside + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sandbox, "config", "containers", "systemd"), 0o755); err != nil {
		t.Fatal(err)
	}

	paths := podman.ExtraVolumePaths()
	if len(paths) != 1 || paths[0] != outside {
		t.Fatalf("ExtraVolumePaths() = %v, want [%s]", paths, outside)
	}

	// Seed the quadlet the way RewriteFPMQuadlets does: template + extra mounts.
	tmpl, err := podman.GetQuadletTemplate("servlo-nginx.container")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := podman.WriteQuadletDiff("servlo-nginx", podman.InjectExtraVolumes(tmpl, paths)); err != nil {
		t.Fatal(err)
	}

	quadlet := filepath.Join(sandbox, "config", "containers", "systemd", "servlo-nginx.container")
	want := "Volume=" + outside + ":" + outside + ":rw"
	before, err := os.ReadFile(quadlet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), want) {
		t.Fatalf("seeded quadlet is missing %q:\n%s", want, before)
	}

	// Saving a global nginx http config rewrites the quadlet (internal/ui/nginx_global.go).
	if _, err := RewriteNginxQuadlet(); err != nil {
		t.Fatal(err)
	}

	after, err := os.ReadFile(quadlet)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), want) {
		t.Errorf("RewriteNginxQuadlet dropped the out-of-home mount %q; nginx can no longer read the site:\n%s", want, after)
	}
}
