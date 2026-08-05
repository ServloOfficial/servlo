//go:build linux

package cli

import "github.com/realrashid/servlo/internal/feedback"

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
