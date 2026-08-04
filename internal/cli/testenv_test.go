package cli

import (
	"path/filepath"
	"testing"
)

// withTempXDG points HOME and the XDG dirs at a temp tree so a test never
// reads or writes the developer's real servlo config and data.
func withTempXDG(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
}
