// Package version holds build-time version information injected via ldflags.
package version

import "fmt"

// These variables are set at build time via:
//
//	-X github.com/realrashid/servlo/internal/version.Version=<tag>
//	-X github.com/realrashid/servlo/internal/version.Commit=<sha>
//	-X github.com/realrashid/servlo/internal/version.Date=<iso8601>
//
// The fallback when nothing is injected. Servlo has not released, so it starts
// at 0.1.0 rather than carrying over the version it was forked at, which would
// have claimed a release history Servlo does not have and which the update
// checker compares against.
//
// 0.y.z already means anything may change, which is the whole statement. No
// prerelease tag: "beta" is a thing to tell a person, and the panel says it in
// words, whereas a -beta suffix in the version string is a thing every tag
// comparison and package manager has to parse.
var (
	Version = "0.1.0"
	Commit  = "none"
	Date    = "unknown"
)

// String returns the full version string shown by `servlo --version`.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
