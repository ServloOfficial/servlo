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
// ExecStart lines point at wherever servlo is actually installed: ~/.local/bin
// for curl/brew, /usr/bin for the deb. A var so tests can override it; returns
// "" when the path can't be resolved, in which case GetUnit leaves the template
// default in place.
var servloBinaryPath = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, rErr := filepath.EvalSymlinks(exe); rErr == nil {
		return stableBinaryPath(exe, resolved)
	}
	return exe
}

// stableBinaryPath picks which spelling of the binary a unit should carry.
// Normally the resolved one, so a symlink that moves cannot break the unit. A
// Homebrew Cellar is the exception: it is version-pinned, so resolving there
// writes a path that `brew upgrade servlo` deletes, leaving the daemons pointing
// at a binary that no longer exists. Homebrew's own symlink is the stable name,
// so that one is kept.
func stableBinaryPath(exe, resolved string) string {
	if strings.Contains(resolved, "/Cellar/") {
		return exe
	}
	return resolved
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
