//go:build linux

package cli

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/ServloOfficial/servlo/internal/serverbasics"
)

// stubBootstrapSystem redirects the root actions runBootstrapSystem performs so
// the test never touches /etc or the real login manager.
func stubBootstrapSystem(t *testing.T) *[][]string {
	t.Helper()
	origPath, origRunner, origBasics := unprivPortDropIn, bootstrapRunner, serverBasicsFn
	t.Cleanup(func() {
		unprivPortDropIn, bootstrapRunner, serverBasicsFn = origPath, origRunner, origBasics
	})
	unprivPortDropIn = filepath.Join(t.TempDir(), "99-servlo-ports.conf")
	// A machine that already has every basic, so these tests measure the
	// bootstrap sequence rather than the host they run on.
	serverBasicsFn = func() []serverbasics.Plan { return nil }

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
//
// The server basics are stubbed out here and covered by their own test below;
// what this pins is that ports come first and linger last.
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

// The server basics are applied by the same root pass, so a droplet set up
// through a package script ends up with swap and fail2ban without anyone
// running four more commands by hand.
func TestRunBootstrapSystemAppliesTheServerBasics(t *testing.T) {
	runs := stubBootstrapSystem(t)
	serverBasicsFn = func() []serverbasics.Plan {
		return []serverbasics.Plan{
			{Name: "swap", Commands: []string{"mkswap /swapfile"}},
			{Name: "timezone", Satisfied: true, Detail: "already UTC"},
		}
	}

	if err := runBootstrapSystem(""); err != nil {
		t.Fatalf("runBootstrapSystem: %v", err)
	}
	var sawSwap bool
	for _, run := range *runs {
		if len(run) == 3 && run[0] == "sh" && run[2] == "mkswap /swapfile" {
			sawSwap = true
		}
	}
	if !sawSwap {
		t.Errorf("the swap command was never run: %v", *runs)
	}
}

// One basic failing must not stop the others. These are independent
// improvements to a machine, and a droplet whose mirror has no fail2ban should
// still come out of this with swap.
func TestRunBootstrapSystemCarriesOnPastAFailingBasic(t *testing.T) {
	runs := stubBootstrapSystem(t)
	serverBasicsFn = func() []serverbasics.Plan {
		return []serverbasics.Plan{
			{Name: "first", Commands: []string{"false"}},
			{Name: "second", Commands: []string{"echo second"}},
		}
	}
	origRunner := bootstrapRunner
	bootstrapRunner = func(name string, args ...string) error {
		if err := origRunner(name, args...); err != nil {
			return err
		}
		if len(args) == 2 && args[1] == "false" {
			return errFakeBasicFailure
		}
		return nil
	}

	if err := runBootstrapSystem(""); err != nil {
		t.Fatalf("runBootstrapSystem: %v", err)
	}
	var sawSecond bool
	for _, run := range *runs {
		if len(run) == 3 && run[2] == "echo second" {
			sawSecond = true
		}
	}
	if !sawSecond {
		t.Errorf("a failing basic stopped the ones after it: %v", *runs)
	}
}

var errFakeBasicFailure = fmt.Errorf("simulated failure")
