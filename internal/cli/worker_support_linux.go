//go:build linux

package cli

import "github.com/realrashid/servlo/internal/config"

// workerSupportedOnPlatform reports whether the given worker can run on the
// current host. Linux supports every shape — host workers go through fnm +
// systemd user units; scheduled workers via .timer + Type=oneshot — so the
// gate is unconditionally permissive.
//
// This is a package var (not a function) so tests can substitute it to
// exercise the unsupported path without a darwin build. The macOS build
// Linux has no platform variant to install.
var workerSupportedOnPlatform = func(_ config.FrameworkWorker) (bool, string) {
	return true, ""
}
