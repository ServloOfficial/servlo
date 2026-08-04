// Package version holds build-time version information injected via ldflags.
package version

import "fmt"

// These variables are set at build time via:
//
//	-X github.com/realrashid/servlo/internal/version.Version=<tag>
//	-X github.com/realrashid/servlo/internal/version.Commit=<sha>
//	-X github.com/realrashid/servlo/internal/version.Date=<iso8601>
var (
	Version = "1.31.0"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string shown by `servlo --version`.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
