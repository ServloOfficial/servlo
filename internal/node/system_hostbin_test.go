package node

import (
	"path/filepath"
	"testing"
)

// withExtraPrefix points the unit PATH list at a fixture standing in for
// an install prefix a daemon never has on PATH.
func withExtraPrefix(t *testing.T, dir string) {
	t.Helper()
	prev := unitPathDirs
	unitPathDirs = func() []string { return []string{dir} }
	t.Cleanup(func() { unitPathDirs = prev })
}

// A host worker unit written by servlo-panel or servlo-watcher must resolve the same
// Node the CLI does. Under the daemon's PATH that prefix is invisible, and
// falling through to an old nvm install would run the worker on a different
// Node than `servlo npm` installed its modules with.
func TestSystemNodeBinDirs_findsExtraPrefixUnderDaemonPATH(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", "")

	prefix := filepath.Join(tmp, "extra-prefix", "bin")
	writeExec(t, prefix, "node")
	writeExec(t, prefix, "npm")
	withExtraPrefix(t, prefix)

	// An older nvm install, the thing the daemon used to fall back to.
	stale := filepath.Join(home, ".nvm", "versions", "node", "v16.20.1", "bin")
	writeExec(t, stale, "node")
	writeExec(t, stale, "npm")

	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	dirs := SystemNodeBinDirs()
	if len(dirs) != 1 || dirs[0] != prefix {
		t.Fatalf("want [%s], got %v", prefix, dirs)
	}
}

// With no node in an extra prefix, the version-manager fallback still applies.
func TestSystemNodeBinDirs_stillFallsBackToNvm(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", home)
	t.Setenv("NVM_DIR", "")

	withExtraPrefix(t, filepath.Join(tmp, "empty-prefix"))

	nvmBin := filepath.Join(home, ".nvm", "versions", "node", "v22.1.0", "bin")
	writeExec(t, nvmBin, "node")
	writeExec(t, nvmBin, "npm")

	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")

	dirs := SystemNodeBinDirs()
	if len(dirs) != 1 || dirs[0] != nvmBin {
		t.Fatalf("want [%s], got %v", nvmBin, dirs)
	}
}

// A real PATH entry still wins over the extra prefixes, so a user who puts a
// specific Node first in their shell keeps it.
func TestSystemNodeBinDirs_pathWinsOverExtraDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	t.Setenv("NVM_DIR", "")

	onPath := filepath.Join(tmp, "chosen")
	writeExec(t, onPath, "node")
	writeExec(t, onPath, "npm")
	prefix := filepath.Join(tmp, "extra-prefix", "bin")
	writeExec(t, prefix, "node")
	writeExec(t, prefix, "npm")
	withExtraPrefix(t, prefix)

	t.Setenv("PATH", onPath)

	dirs := SystemNodeBinDirs()
	if len(dirs) != 1 || dirs[0] != onPath {
		t.Fatalf("want [%s], got %v", onPath, dirs)
	}
}
