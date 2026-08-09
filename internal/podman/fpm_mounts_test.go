package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// Every host path the FPM container mounts has to exist before it starts.
//
// Podman refuses a bind mount whose source is missing, with a statfs error and
// exit 125, and systemd restarting the unit cannot conjure a file. The pool
// directory and the production drop-in were both written by something later
// than the quadlet, so a fresh install started with no shared FPM at all and a
// warning naming a path rather than what was supposed to create it. It healed
// on the first `servlo link`, which is why it read as noise rather than as the
// install leaving PHP down.
//
// Derived from the rendered quadlet rather than from a list, because a list
// beside the template is a list that goes stale the next time a mount is added.
func TestWriteFPMQuadlet_createsEveryMountItDeclares(t *testing.T) {
	sandbox := t.TempDir()
	t.Setenv("HOME", filepath.Join(sandbox, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sandbox, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(sandbox, "data"))

	reload := DaemonReloadFn
	DaemonReloadFn = func() error { return nil }
	t.Cleanup(func() { DaemonReloadFn = reload })

	const version = "8.4"
	if err := WriteFPMQuadlet(version); err != nil {
		t.Fatalf("WriteFPMQuadlet: %v", err)
	}

	content, err := renderFPMQuadletContent(version)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "Volume=") {
			continue
		}
		src, _, ok := strings.Cut(strings.TrimPrefix(line, "Volume="), ":")
		// Only the paths under servlo's own data directory. The home mount and
		// the named ssh-agent volume are podman's to make.
		if !ok || !strings.HasPrefix(src, config.DataDir()+string(filepath.Separator)) {
			continue
		}
		checked++
		if _, err := os.Stat(src); err != nil {
			t.Errorf("the FPM quadlet mounts %s and nothing creates it, so podman refuses to start the container: %v", src, err)
		}
	}
	if checked == 0 {
		t.Fatal("no mounts were checked, so this proves nothing")
	}
}
