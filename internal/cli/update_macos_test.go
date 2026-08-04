package cli

import (
	"runtime"
	"strings"
	"testing"
)

// /usr/local is an ordinary install prefix on macOS, not a package manager's
// territory: there is no apt or dnf to defer to, so treating it as packaged
// leaves `servlo update` and `servlo uninstall` with nothing to suggest.
func TestSystemPackageManagedIsLinuxOnly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	for _, path := range []string{
		"/usr/local/bin/servlo",
		"/usr/bin/servlo",
		"/opt/homebrew/Cellar/servlo/1.31.0/bin/servlo",
	} {
		if isSystemPackageManaged(path) {
			t.Errorf("isSystemPackageManaged(%q) = true on macOS", path)
		}
	}
}

// Nix is real on macOS too, and a /nix/store binary genuinely must not be
// self-replaced.
func TestNixStaysPackageManagedEverywhere(t *testing.T) {
	if !isSystemPackageManaged("/nix/store/abc123-servlo-1.31.0/bin/servlo") {
		t.Error("a /nix/store binary must stay package-managed")
	}
}

func TestPackageManagerHintsPointAtBrewOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	self := "/opt/homebrew/Cellar/servlo/1.31.0/bin/servlo"
	if got := packageManagerUpdateHint(self); !strings.Contains(got, "brew upgrade") {
		t.Errorf("packageManagerUpdateHint() = %q; want a brew command", got)
	}
	if got := packageManagerRemoveHint(self); !strings.Contains(got, "brew uninstall") {
		t.Errorf("packageManagerRemoveHint() = %q; want a brew command", got)
	}
}
