// Package version holds build-time version information injected via ldflags.
package version

import "fmt"

// These variables are set at build time via:
//
//	-X github.com/realrashid/servlo/internal/version.Version=<tag>
//	-X github.com/realrashid/servlo/internal/version.Commit=<sha>
//	-X github.com/realrashid/servlo/internal/version.Date=<iso8601>
//
// The fallback when nothing is injected. Servlo is a fork that has not
// released, so it starts at 0.1.0 rather than inheriting upstream's number:
// 1.31.0 would claim thirty-one minor releases of Servlo that never happened,
// and the update checker compares against it.
//
// 0.y.z already means anything may change. The -beta.1 says so out loud, and
// sorts before 0.1.0, so the first stable release is a clean v0.1.0.
var (
	Version = "0.1.0-beta.1"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string shown by `servlo --version`.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
