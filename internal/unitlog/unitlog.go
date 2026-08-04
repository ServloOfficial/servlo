// Package unitlog centralises how servlo locates and reads a unit's logs:
// whether a unit runs as a detached podman container (logs via `podman logs`)
// or as a systemd user unit (logs in the journal), the framework-worker
// classification that decision leans on, and the text rules the log readers
// share.
package unitlog

import (
	"regexp"
	"strings"
)

// IsFrameworkWorkerUnit reports whether unit looks like a built-in framework
// worker (queue, schedule, horizon, reverb).
func IsFrameworkWorkerUnit(unit string) bool {
	for _, prefix := range []string{"servlo-queue-", "servlo-schedule-", "servlo-horizon-", "servlo-reverb-"} {
		if strings.HasPrefix(unit, prefix) {
			return true
		}
	}
	return false
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI colour escapes so log text stays readable wherever it
// is rendered.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }
