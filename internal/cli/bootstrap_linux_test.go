//go:build linux

package cli

import (
	"path/filepath"
	"testing"
)

// stubBootstrapSystem redirects the root actions runBootstrapSystem performs so
// the test never touches /etc or the real login manager.
func stubBootstrapSystem(t *testing.T) *[][]string {
	t.Helper()
	origPath, origRunner := unprivPortDropIn, bootstrapRunner
	t.Cleanup(func() {
		unprivPortDropIn, bootstrapRunner = origPath, origRunner
	})
	unprivPortDropIn = filepath.Join(t.TempDir(), "99-servlo-ports.conf")

	var runs [][]string
	bootstrapRunner = func(name string, args ...string) error {
		runs = append(runs, append([]string{name}, args...))
		return nil
	}
	return &runs
}

// This runs as root already, called from a package maintainer script or from an
// install re-executing itself under sudo, so unlike the install's own port step
// it applies the sysctl rather than printing it.
func TestRunBootstrapSystemAppliesPortsAndLinger(t *testing.T) {
	runs := stubBootstrapSystem(t)

	if err := runBootstrapSystem("george"); err != nil {
		t.Fatalf("runBootstrapSystem: %v", err)
	}
	if len(*runs) != 2 || (*runs)[0][0] != "sysctl" || (*runs)[1][0] != "loginctl" {
		t.Errorf("commands = %v, want sysctl then loginctl", *runs)
	}
}

func TestRunBootstrapSystemNoTargetUser(t *testing.T) {
	runs := stubBootstrapSystem(t)

	if err := runBootstrapSystem(""); err != nil {
		t.Fatalf("runBootstrapSystem: %v", err)
	}
	// Ports are machine-global and still apply; linger is per-user and has
	// nobody to apply to.
	if len(*runs) != 1 || (*runs)[0][0] != "sysctl" {
		t.Errorf("commands = %v, want sysctl only", *runs)
	}
}
