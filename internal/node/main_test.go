package node

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/hostbin"
)

// The Node resolution tests build a whole host layout under a temp HOME and a
// PATH of their own. The real install prefixes
// would let whatever the developer's machine has installed decide the result,
// so they are empty by default and each test that needs them opts in.
// Servlo's own state goes to a temp directory for the same reason: reading
// global config resolves the presets, and resolving one writes this install's
// service password.
func TestMain(m *testing.M) {
	hostbin.ExtraDirs = func() []string { return nil }
	cleanup := config.IsolateStateForTests()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
