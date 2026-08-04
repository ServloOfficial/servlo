package cli

import (
	"github.com/realrashid/servlo/internal/services"
)

// recordingMgr embeds the real ServiceManager interface (nil here, since only
// IsEnabled is exercised on the path under test) and records which unit names
// regenerateWorkerUnit probes via IsEnabled. Returning false makes
// regenerateWorkerUnit return early, so no heavier lifecycle method runs.
type recordingMgr struct {
	services.ServiceManager
	enabledProbed map[string]bool
}

func (m *recordingMgr) IsEnabled(name string) bool {
	m.enabledProbed[name] = true
	return false
}
