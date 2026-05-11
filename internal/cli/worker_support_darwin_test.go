//go:build darwin

package cli

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/config"
)

// TestWorkerSupportedOnPlatform_darwin pins the darwin support matrix
// after host:true became a first-class shape via writeWorkerHostUnit.
// host workers are accepted; scheduled workers still aren't because
// launchd's StartCalendarInterval isn't wired through the unit
// translator yet. Host + schedule combined fails on the schedule
// branch.
func TestWorkerSupportedOnPlatform_darwin(t *testing.T) {
	cases := []struct {
		name    string
		worker  config.FrameworkWorker
		wantOK  bool
		wantSub string
	}{
		{"plain container worker", config.FrameworkWorker{Command: "true"}, true, ""},
		{"host worker (vite)", config.FrameworkWorker{Command: "npm run dev", Host: true}, true, ""},
		{"scheduled worker", config.FrameworkWorker{Command: "true", Schedule: "*-*-* 03:00:00"}, false, "scheduled"},
		{"host + schedule", config.FrameworkWorker{Command: "true", Host: true, Schedule: "@daily"}, false, "scheduled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ok, reason := workerSupportedOnPlatform(tc.worker)
			if ok != tc.wantOK {
				t.Errorf("got ok=%v, want %v (reason=%q)", ok, tc.wantOK, reason)
			}
			if tc.wantSub != "" && !strings.Contains(reason, tc.wantSub) {
				t.Errorf("expected reason to contain %q, got %q", tc.wantSub, reason)
			}
			if tc.wantOK && reason != "" {
				t.Errorf("supported workers should have empty reason, got %q", reason)
			}
		})
	}
}
