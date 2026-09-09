//go:build linux

package cli

import "github.com/ServloOfficial/servlo/internal/config"

// workerSupportedOnPlatform reports whether the given worker can run on the
// current host. Linux supports every shape — host workers go through fnm +
// systemd user units; scheduled workers via .timer + Type=oneshot — so the
// gate is unconditionally permissive.
//
// A package var rather than a function so a test can substitute it and exercise
// the refusal path, which no worker shape servlo supports reaches on its own.
var workerSupportedOnPlatform = func(_ config.FrameworkWorker) (bool, string) {
	return true, ""
}
