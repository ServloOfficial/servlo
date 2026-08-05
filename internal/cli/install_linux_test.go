//go:build linux

package cli

import (
	"errors"
	"strings"
	"testing"
)

// stubSystemSetup drives the preflight conditions and captures both the sudo
// re-exec and the fallback, so the root pass can be exercised without a real
// login manager or sudo. Ports are deliberately absent: they are settled by
// printing commands rather than by escalating.
func stubSystemSetup(t *testing.T, lingerOff bool) (*[][]string, *[]bool) {
	t.Helper()
	origLinger := lingerNeeded
	origRunner, origLegacy := sudoSelfRunner, legacySystemSetup
	t.Cleanup(func() {
		lingerNeeded = origLinger
		sudoSelfRunner, legacySystemSetup = origRunner, origLegacy
	})
	lingerNeeded = func() bool { return lingerOff }

	var runs [][]string
	sudoSelfRunner = func(args ...string) error {
		runs = append(runs, args)
		return nil
	}
	var legacy []bool
	legacySystemSetup = func() error {
		legacy = append(legacy, true)
		return nil
	}
	return &runs, &legacy
}

// The common case is a reinstall where every root step already applies. It must
// stay completely silent rather than asking for a password to do nothing.
func TestRunSystemSetupSkipsWhenAlreadyApplied(t *testing.T) {
	runs, legacy := stubSystemSetup(t, false)

	if err := runSystemSetup(); err != nil {
		t.Fatalf("runSystemSetup: %v", err)
	}
	if len(*runs) != 0 || len(*legacy) != 0 {
		t.Errorf("escalated with nothing to do: sudo %v, fallback %v", *runs, *legacy)
	}
}

func TestRunSystemSetupOneSudoCall(t *testing.T) {
	runs, legacy := stubSystemSetup(t, true)

	if err := runSystemSetup(); err != nil {
		t.Fatalf("runSystemSetup: %v", err)
	}
	if len(*runs) != 1 {
		t.Fatalf("sudo calls = %d, want exactly 1 (%v)", len(*runs), *runs)
	}
	got := strings.Join((*runs)[0], " ")
	if got != "bootstrap --system" {
		t.Errorf("sudo args = %q, want %q", got, "bootstrap --system")
	}
	if len(*legacy) != 0 {
		t.Errorf("fallback ran despite a successful sudo pass: %v", *legacy)
	}
}

// No sudo, or a user who cannot use it, must land on the old per-step path
// rather than failing the install outright.
func TestRunSystemSetupFallsBackWhenSudoFails(t *testing.T) {
	_, legacy := stubSystemSetup(t, true)
	sudoSelfRunner = func(...string) error { return errors.New("sudo: command not found") }

	if err := runSystemSetup(); err != nil {
		t.Fatalf("runSystemSetup: %v", err)
	}
	if len(*legacy) != 1 || !(*legacy)[0] {
		t.Errorf("fallback runs = %v, want one call with DNS managed", *legacy)
	}
}

// The port strategy is applied by printing commands, not by escalating, so a
// host that only needs the sysctl lowered must not trigger the sudo pass. This
// is the S1.2 half of "prints the sudo command rather than running it": if the
// port step could still drag root in, printing would be theatre.
func TestSystemSetupIgnoresPorts(t *testing.T) {
	stubSystemSetup(t, false)

	if systemSetupNeeded() {
		t.Error("systemSetupNeeded = true with only the port strategy outstanding")
	}
}
