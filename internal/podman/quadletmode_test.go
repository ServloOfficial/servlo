package podman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// A quadlet for a database service carries that engine's root password in an
// Environment= line, in plain text. Every other file servlo writes that holds a
// credential is 0600 in a 0700 directory: the service password itself, the SMTP
// accounts, the deploy webhook secrets. The quadlets were 0644 in a 0755
// directory, so the same password every one of those files protects was
// readable by any other account on the machine, from a file, for as long as the
// install existed.
//
// systemd's generator runs as this user, so it loses nothing by the file being
// private.
func TestQuadletsHoldingACredentialAreNotWorldReadable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	const content = "[Container]\nImage=docker.io/library/mariadb:11\n" +
		"Environment=\"MARIADB_ROOT_PASSWORD=hunter2hunter2hunter2\"\n"
	if err := WriteQuadlet("servlo-mariadb", content); err != nil {
		t.Fatalf("writing the quadlet: %v", err)
	}

	path := filepath.Join(config.QuadletDir(), "servlo-mariadb.container")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the quadlet is %04o, so anyone on this machine can read the password in it; want 0600", perm)
	}

	di, err := os.Stat(config.QuadletDir())
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := di.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the quadlet directory is %04o, so its contents are listable by anyone; want 0700", perm)
	}

	// An install made before this had 0644 on disk already, and a rewrite keeps
	// the mode a file already had, so healing it has to be its own step.
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteQuadlet("servlo-mariadb", content); err != nil {
		t.Fatalf("rewriting the quadlet: %v", err)
	}
	fi, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("an existing quadlet kept its %04o, so an install made before this never gets its password covered", perm)
	}

	// The fixture has to actually carry a secret, or this passes for the wrong reason.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hunter2hunter2hunter2") {
		t.Fatal("the fixture lost its password, so this test proves nothing")
	}
}
