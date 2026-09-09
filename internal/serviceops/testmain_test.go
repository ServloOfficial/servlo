package serviceops

import (
	"os"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func TestMain(m *testing.M) {
	// init wires ServiceRunning for production; unit tests want the installed
	// member list unless a case sets the seam itself.
	config.ServiceRunning = nil
	// And servlo's state goes to a temp directory: resolving a preset generates
	// this install's service password on first use.
	cleanup := config.IsolateStateForTests()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
