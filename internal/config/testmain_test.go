package config

import (
	"os"
	"testing"
)

// Servlo's state goes to a temp directory for every test in this package.
// Resolving a preset generates this install's service password on first use, so
// a test that only asks which presets exist writes the developer's.
func TestMain(m *testing.M) {
	cleanup := IsolateStateForTests()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
