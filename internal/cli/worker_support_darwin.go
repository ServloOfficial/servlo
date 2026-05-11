//go:build darwin

package cli

import "github.com/geodro/lerd/internal/config"

// workerSupportedOnPlatform reports whether the given worker can run on
// the current host. host:true workers run natively on macOS via the
// launchd-plist-around-fnm path in writeWorkerHostUnit. Scheduled
// workers (Schedule != "") still lack a macOS path — launchd has
// StartCalendarInterval but the unit translator hasn't been wired
// through services.Mgr yet.
//
// Returning false here makes WorkerStartForSite skip the lifecycle
// calls entirely instead of writing nothing and then trying to
// StartUnit on a unit that was never written.
var workerSupportedOnPlatform = func(w config.FrameworkWorker) (bool, string) {
	if w.Schedule != "" {
		return false, "scheduled workers aren't supported on macOS yet"
	}
	return true, ""
}
