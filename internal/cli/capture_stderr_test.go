package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// runCapturingStdout must capture stderr as well as stdout: subprocesses (npm,
// vite, mysql) write warnings to stderr, and a leaked stderr line clobbers the
// live spinner the setup loop animates on the real stdout.
func TestRunCapturingStdout_CapturesStderr(t *testing.T) {
	out, err := runCapturingStdout(func() error {
		fmt.Fprint(os.Stdout, "on-stdout ")
		fmt.Fprint(os.Stderr, "on-stderr")
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, "on-stdout") {
		t.Errorf("captured = %q, missing stdout", got)
	}
	if !strings.Contains(got, "on-stderr") {
		t.Errorf("captured = %q, missing stderr (it would leak past the spinner)", got)
	}
}

// A bare service entry whose name is a bundled tool preset (phpmyadmin, pgadmin)
// must be resolvable as a preset, while default presets (mysql, redis) must not
// be — they are built-in and handled separately.
func TestBundledToolPresetGate(t *testing.T) {
	if !config.PresetExists("phpmyadmin") || config.IsDefaultPreset("phpmyadmin") {
		t.Error("phpmyadmin should be a non-default bundled preset (resolvable as a preset)")
	}
	if config.PresetExists("mysql") && !config.IsDefaultPreset("mysql") {
		t.Error("mysql is a default preset and must be excluded from tool-preset resolution")
	}
}
