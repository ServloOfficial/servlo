package podman

import (
	"path/filepath"
	"testing"
)

// withTempXDG points HOME and the XDG dirs at a temp tree so a test never
// reads or writes the developer's real servlo config and data. It returns the
// root so a caller can assert on paths beneath it.
func withTempXDG(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	return dir
}
