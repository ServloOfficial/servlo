//go:build darwin

package cli

import (
	"os"
	"path/filepath"

	"github.com/realrashid/servlo/internal/feedback"
)

const servloCleanupScript = `#!/bin/sh
# servlo-cleanup — standalone Servlo uninstaller for macOS.
# Run this if you already removed the servlo binary via brew uninstall servlo
# and need to clean up services, launch agents, and DNS config.

set -e

echo "==> Servlo cleanup"

# ── Stop and bootout all servlo launchd services ──────────────────────────────
DOMAIN="gui/$(id -u)"
for plist in "$HOME/Library/LaunchAgents/servlo-"*.plist; do
  [ -f "$plist" ] || continue
  label=$(defaults read "$plist" Label 2>/dev/null)
  [ -n "$label" ] || continue
  echo "  --> Stopping $label"
  launchctl bootout "$DOMAIN/$label" 2>/dev/null || true
done

# ── Remove servlo launch agent plists ─────────────────────────────────────────
echo "  --> Removing launch agents"
rm -f "$HOME/Library/LaunchAgents/servlo-"*.plist

# ── Stop and remove servlo podman containers ───────────────────────────────────
if command -v podman >/dev/null 2>&1; then
  for ctr in $(podman ps -a --format '{{.Names}}' 2>/dev/null | grep '^servlo-' || true); do
    echo "  --> Removing container $ctr"
    podman stop "$ctr" 2>/dev/null || true
    podman rm -f "$ctr" 2>/dev/null || true
  done
  echo "  --> Removing servlo podman network"
  podman network rm servlo 2>/dev/null || true
fi

# ── Remove /etc/resolver entry ───────────────────────────────────────────────
if [ -f /etc/resolver/test ]; then
  echo "  --> Removing /etc/resolver/test (requires sudo)"
  sudo rm -f /etc/resolver/test
fi

# ── Remove log files ─────────────────────────────────────────────────────────
rm -rf "$HOME/Library/Logs/servlo"

echo ""
printf "  Remove config and data (~/.config/servlo, ~/.local/share/servlo)? [y/N] "
read -r ans
case "$ans" in
  [Yy]|[Yy][Ee][Ss])
    rm -rf "$HOME/.config/servlo" "$HOME/.local/share/servlo"
    echo "  --> Config and data removed."
    ;;
  *)
    echo "  --> Config and data kept."
    ;;
esac

echo ""
echo "Servlo cleanup complete."
echo "Run 'brew uninstall servlo' if you haven't already."
`

// installCleanupScript writes a standalone shell uninstaller to ~/.local/bin/servlo-cleanup.
// This lets macOS users clean up servlo's launchd agents, containers, and DNS config
// even if they already removed the servlo binary via `brew uninstall servlo`.
func installCleanupScript() {
	binDir := filepath.Join(os.Getenv("HOME"), ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		feedback.Warn("could not create %s: %v", binDir, err)
		return
	}
	dest := filepath.Join(binDir, "servlo-cleanup")
	if err := os.WriteFile(dest, []byte(servloCleanupScript), 0755); err != nil {
		feedback.Warn("could not write servlo-cleanup script: %v", err)
	}
}
