package sitetpl

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

// Servlo's state goes to a temp directory for every test in this package. The
// path that reaches it is several calls deep: reading global config resolves the
// default presets, and resolving one generates this install's service password
// on first use, so a test that never mentions passwords writes the developer's.
func TestMain(m *testing.M) {
	cleanup := config.IsolateStateForTests()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
