package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// realStateDirs are the servlo dirs of whoever is running the process, resolved once
// at start. XDG_DATA_HOME and XDG_CONFIG_HOME decide them and a test moves those,
// so resolving later can't tell a test's temp dir from the developer's own. The
// unit dirs are here too: the bug that started this wrote a real systemd unit.
var realStateDirs = []string{DataDir(), ConfigDir(), SystemdUserDir(), QuadletDir()}

// underTest reports whether this process is a test binary, read from the command
// line rather than testing.Testing() so the std testing package stays out of the
// shipped servlo binary.
var underTest = func() bool {
	if strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], "/_test/") {
		return true
	}
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-test.") {
			return true
		}
	}
	return false
}()

// GuardRealWrite is guardRealWrite for writers and removers in other packages.
func GuardRealWrite(path string) { guardRealWrite(path) }

// guardRealWrite stops a test writing or deleting the state of the developer
// running it. A test that never isolated XDG_DATA_HOME has emptied a real
// sites.yaml, and another deleted a real quadlet, leaving its container running
// under a unit systemd no longer had a definition for. Make either a loud failure
// rather than silent damage.
func guardRealWrite(path string) {
	if !underTest {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	abs = resolveLinks(abs)
	for _, real := range realStateDirs {
		if real == "" {
			continue
		}
		real = resolveLinks(real)
		if abs == real || strings.HasPrefix(abs, real+string(os.PathSeparator)) {
			panic(fmt.Sprintf("test wrote to or removed the real servlo state at %s: isolate it with "+
				`t.Setenv("XDG_DATA_HOME", t.TempDir())`+" and "+`t.Setenv("XDG_CONFIG_HOME", t.TempDir())`, abs))
		}
	}
}

// resolveLinks canonicalises what exists of path, so a symlinked state dir can't
// slip past the prefix comparison.
func resolveLinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// UnderTest reports whether this process is a test binary, for packages that must
// refuse to touch the real system (systemd, podman) when no stub is installed.
func UnderTest() bool { return underTest }

// IsolateStateForTests points servlo's data and config directories at a fresh
// temp directory and returns the function that removes it. For a TestMain that
// needs a whole package kept off the real install, rather than the per-test
// t.Setenv pair, because the path that reaches state is often several calls deep:
// reading global config resolves the default presets, and resolving a preset
// generates this install's service password on first use. A test that never
// mentions passwords writes one.
//
// The guard still holds after this: realStateDirs is resolved when the package
// loads, which is before any TestMain runs.
func IsolateStateForTests() func() {
	dir, err := os.MkdirTemp("", "servlo-test-state-")
	if err != nil {
		panic("isolating servlo state for tests: " + err.Error())
	}
	os.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	return func() { os.RemoveAll(dir) }
}
