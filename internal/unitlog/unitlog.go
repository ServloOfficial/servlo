// Package unitlog centralises how servlo locates a unit's logs across platforms:
// whether a unit runs as a detached podman container (logs via `podman logs`)
// versus a launchd-supervised service on macOS (logs in ~/Library/Logs/servlo),
// and the framework-worker classification both decisions lean on. Shared by the
// UI log streamer and the logsource reader so the rules live in one place.
package unitlog

import "strings"

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
