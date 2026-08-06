//go:build linux

package cli

import (
	"fmt"

	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/serverbasics"
)

// runBootstrapSystem performs the root prerequisites the per-user install relies
// on: the unprivileged-port sysctl and systemd linger. It runs as root already,
// called by a package maintainer script or by an interactive install
// re-executing itself under sudo, so unlike the install's own port step it
// applies rather than prints.
func runBootstrapSystem(target string) error {
	feedback.Header("Bootstrapping system for servlo")

	if err := writePortDropIn(unprivPortDropIn, bootstrapRunner); err != nil {
		feedback.Warn("enabling unprivileged ports: %v", err)
	} else {
		feedback.Done("unprivileged ports enabled for 80/443")
	}

	applyServerBasics()

	if target == "" {
		return nil
	}
	if err := enableLinger(target, bootstrapRunner); err != nil {
		feedback.Warn("enabling linger for %s: %v", target, err)
	} else {
		feedback.Done("systemd linger enabled for " + target)
	}
	return nil
}

// serverBasicsFn reads what this machine still needs. A seam so a test can
// drive the bootstrap sequence without its result depending on the memory,
// timezone and installed packages of whatever machine is running it.
var serverBasicsFn = serverbasics.All

// applyServerBasics runs the swap, timezone, unattended-upgrades and fail2ban
// steps. This is the one place they are executed rather than printed, because
// bootstrap is already root: it is called by a package maintainer script or by
// an install re-executing itself under sudo, so there is no prompt to raise and
// no privilege to ask for.
//
// A step that fails is reported and the rest carry on. These are independent
// improvements to a machine, and a droplet with no fail2ban package in its
// mirror should still end up with swap.
func applyServerBasics() {
	for _, plan := range serverBasicsFn() {
		if plan.Satisfied {
			feedback.Done(plan.Name + ": " + plan.Detail)
			continue
		}
		if len(plan.Commands) == 0 {
			feedback.Warn("%s: %s", plan.Name, plan.Detail)
			continue
		}
		if err := runBasicCommands(plan.Commands); err != nil {
			feedback.Warn("%s: %v", plan.Name, err)
			continue
		}
		feedback.Done(plan.Name + " configured")
	}
}

// runBasicCommands executes a plan's commands. They are written as shell lines
// because several are pipelines into tee, so they run through sh rather than
// being split into argv. The strings are built in this repository from typed
// values, never from anything an operator or a site supplies.
func runBasicCommands(commands []string) error {
	for _, c := range commands {
		if err := bootstrapRunner("sh", "-c", c); err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
	}
	return nil
}
