//go:build darwin

package cli

import (
	"os"

	"github.com/realrashid/servlo/internal/feedback"
	"github.com/realrashid/servlo/internal/services"
	servloSystemd "github.com/realrashid/servlo/internal/systemd"
)

// installAutostart enables the servlo-autostart launchd service on macOS so that
// servlo starts automatically on every login. On macOS this is on by default
// (matching Herd's behaviour); on Linux it is opt-in via `servlo autostart enable`.
func installAutostart() {
	content, err := servloSystemd.GetUnit("servlo-autostart")
	if err != nil {
		feedback.WarnOn(os.Stderr, "autostart unit: %v", err)
		return
	}
	if err := services.Mgr.WriteServiceUnit("servlo-autostart", content); err != nil {
		feedback.WarnOn(os.Stderr, "writing autostart service: %v", err)
		return
	}
	if err := services.Mgr.Enable("servlo-autostart"); err != nil {
		feedback.WarnOn(os.Stderr, "enabling autostart: %v", err)
	}
}
