//go:build linux

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A per cent sign is a systemd specifier, expanded before anything runs, and
// doubling it is how a unit says it meant the character. internal/sitecron says
// so beside its own escaper; the worker units, which carry the same kind of
// operator-written command, wrote it through.
//
// Nearly every letter is a specifier, so what happens depends on which one
// follows: an unknown one fails the unit outright, and a known one quietly
// substitutes something. %h is the home directory, which is how a log format
// string turns into a path with nobody told.
func TestWriteWorkerUnitFile_EscapesASpecifierInTheCommand(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := writeWorkerUnitFile(
		"servlo-custom-mysite", "Custom", "mysite", t.TempDir(), "8.4",
		`gunicorn app:app --access-logformat '%(h)s %(r)s'`, "always", "", "servlo-php84-fpm", false,
	); err != nil {
		t.Fatalf("writeWorkerUnitFile: %v", err)
	}
	assertNoBareSpecifier(t, filepath.Join(tmp, "systemd", "user", "servlo-custom-mysite.service"))
}

// The host worker is the one a host-proxy site's own command runs through, and
// the comment above it says the wrapper carries a user-provided string verbatim.
func TestWriteHostWorkerUnitFile_EscapesASpecifierInTheCommand(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("XDG_DATA_HOME", tmp)

	if _, err := writeWorkerUnitFile(
		"servlo-serve-mysite", "Serve", "mysite", t.TempDir(), "",
		`gunicorn app:app --access-logformat '%(h)s %(r)s'`, "always", "", "", true,
	); err != nil {
		t.Fatalf("writeWorkerUnitFile (host): %v", err)
	}
	assertNoBareSpecifier(t, filepath.Join(tmp, "systemd", "user", "servlo-serve-mysite.service"))
}

// assertNoBareSpecifier fails when the unit's ExecStart carries a per cent sign
// systemd would read as a specifier rather than as the character the operator
// typed.
func assertNoBareSpecifier(t *testing.T, unitPath string) {
	t.Helper()
	b, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "ExecStart=") {
			continue
		}
		rest := line
		for {
			i := strings.Index(rest, "%")
			if i < 0 {
				return
			}
			if i+1 >= len(rest) || rest[i+1] != '%' {
				t.Fatalf("ExecStart carries a bare specifier, so systemd expands it before the command runs:\n%s", line)
			}
			rest = rest[i+2:]
		}
	}
	t.Fatalf("no ExecStart in %s", unitPath)
}
