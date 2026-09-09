package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The places that decide whether a site's workers exist at all have to resolve
// through FrameworkForSite.
//
// GetFrameworkForDir answers "which framework is this directory on", and for a
// custom-container site the honest answer is none: it runs an image the operator
// brought. Its workers are real all the same, and they live in its own
// .servlo.yaml. Each file below acts on that set, and each one that reached for
// GetFrameworkForDir instead was silently acting on an empty one. The panel
// listed workers on such a site and refused to start any of them; the boot sweep
// wrote none of their unit files while start went on enumerating them; pausing
// the site left them running.
//
// A file here that goes back to GetFrameworkForDir for its worker set is the
// same bug again, and it is not a bug any test of a worker will catch, because
// the sites it happens on are the ones no fixture has.
func TestSiteWorkerCallers_ResolveThroughFrameworkForSite(t *testing.T) {
	root := repoRoot(t)
	for path, what := range map[string]string{
		"internal/ui/server.go":         "the panel's worker start handler",
		"internal/cli/startstop.go":     "the boot sweep that writes worker units",
		"internal/cli/pause.go":         "pausing and resuming a site's workers",
		"internal/cli/worker.go":        "the worker CLI",
		"internal/siteinfo/siteinfo.go": "the worker list the dashboard renders",
	} {
		src, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !strings.Contains(string(src), "config.FrameworkForSite(") {
			t.Errorf("%s (%s) no longer calls config.FrameworkForSite, so a custom-container site's own workers are invisible to it", path, what)
		}
	}
}
