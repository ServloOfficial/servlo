package systemd

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed units
var unitsFS embed.FS

// servloBinaryPath resolves the absolute path to the running servlo binary so unit
// ExecStart lines point at wherever servlo is actually installed: ~/.local/bin for
// the installer, /usr/bin for the deb. Symlinks are resolved so a link that later
// moves cannot break the unit. A var so tests can override it; returns "" when the
// path can't be resolved, in which case GetUnit leaves the template default in
// place.
var servloBinaryPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, rErr := filepath.EvalSymlinks(exe); rErr == nil {
		return resolved
	}
	return exe
}

// GetUnit returns the content of an embedded systemd unit file with the servlo
// binary path resolved. The templates ship with ExecStart=%h/.local/bin/servlo,
// which only works for a ~/.local/bin install; substituting the real path lets
// the daemon units run from any install location (notably /usr/bin under the
// Debian package).
func GetUnit(name string) (string, error) {
	// name may or may not have .service suffix
	filename := name
	if !strings.HasSuffix(filename, ".service") {
		filename += ".service"
	}
	data, err := unitsFS.ReadFile("units/" + filename)
	if err != nil {
		return "", fmt.Errorf("systemd unit %q not found: %w", name, err)
	}
	return resolveUnitBinaryPath(string(data)), nil
}

// resolveUnitBinaryPath swaps the hardcoded ~/.local/bin template for the
// running binary's real location.
func resolveUnitBinaryPath(content string) string {
	bin := servloBinaryPath()
	if bin == "" {
		return content
	}
	return strings.ReplaceAll(content, "%h/.local/bin/servlo", bin)
}
