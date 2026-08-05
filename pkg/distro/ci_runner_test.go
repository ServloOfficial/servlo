package distro

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// The CI gate has to run on the release this package is the gate for. Pinning it
// here rather than in the workflow alone is what stops the two drifting: a
// runner left on ubuntu-latest follows the runner image to the next LTS, and
// from that point CI is proving Servlo builds on a platform the installer
// refuses to install on.
func TestCIWorkflowRunsOnTheSupportedUbuntu(t *testing.T) {
	const path = "../../.github/workflows/ci.yml"

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the CI workflow: %v", err)
	}

	want := fmt.Sprintf("ubuntu-%d.%02d", MinUbuntuMajor, MinUbuntuMinor)

	var found int
	for i, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "runs-on:") {
			continue
		}
		found++
		got := strings.TrimSpace(strings.TrimPrefix(trimmed, "runs-on:"))
		if got != want {
			t.Errorf("%s:%d: runs on %q, want %q", path, i+1, got, want)
		}
	}
	if found == 0 {
		t.Fatalf("%s declares no runner at all", path)
	}
}
