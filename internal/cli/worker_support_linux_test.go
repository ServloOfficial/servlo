//go:build linux

package cli

import (
	"testing"

	"github.com/realrashid/servlo/internal/config"
)

// TestWorkerSupportedOnPlatform_linuxAlwaysOK pins that Linux supports
// every worker shape: host, scheduled, both. The platform gate exists
// only where the service manager lacks parity for these.
func TestWorkerSupportedOnPlatform_linuxAlwaysOK(t *testing.T) {
	cases := []config.FrameworkWorker{
		{Command: "true"},
		{Command: "true", Host: true},
		{Command: "true", Schedule: "*-*-* 03:00:00"},
		{Command: "true", Host: true, Schedule: "@daily"},
	}
	for i, w := range cases {
		ok, reason := workerSupportedOnPlatform(w)
		if !ok {
			t.Errorf("case %d (%+v): unexpected unsupported, reason=%q", i, w, reason)
		}
		if reason != "" {
			t.Errorf("case %d: unexpected non-empty reason %q", i, reason)
		}
	}
}
