package systemd

import (
	"strings"
	"testing"
)

func TestGetUnitResolvesBinaryPath(t *testing.T) {
	orig := servloBinaryPath
	t.Cleanup(func() { servloBinaryPath = orig })
	servloBinaryPath = func() string { return "/usr/bin/servlo" }

	ui, err := GetUnit("servlo-panel")
	if err != nil {
		t.Fatalf("GetUnit: %v", err)
	}
	if !strings.Contains(ui, "ExecStart=/usr/bin/servlo serve-ui") {
		t.Errorf("servlo-panel ExecStart not resolved:\n%s", ui)
	}
	if strings.Contains(ui, "%h/.local/bin/servlo") {
		t.Errorf("servlo-panel still has the template path:\n%s", ui)
	}
}

// When the binary path can't be resolved, the template default is left intact
// rather than producing a broken ExecStart.
func TestGetUnitKeepsTemplateWhenUnresolved(t *testing.T) {
	orig := servloBinaryPath
	t.Cleanup(func() { servloBinaryPath = orig })
	servloBinaryPath = func() string { return "" }

	ui, err := GetUnit("servlo-panel")
	if err != nil {
		t.Fatalf("GetUnit: %v", err)
	}
	if !strings.Contains(ui, "%h/.local/bin/servlo serve-ui") {
		t.Errorf("expected template default, got:\n%s", ui)
	}
}
