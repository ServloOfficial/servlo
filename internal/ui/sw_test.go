package ui

import (
	"strings"
	"testing"
)

// The profiler UI is served under /_spx/ and is dynamic — its report list
// updates as profiles are captured. The dashboard service worker is
// cache-first, so it must bypass /_spx/ or it pins a stale (often empty)
// report list that no header can dislodge (the Cache API ignores them).
func TestServiceWorker_BypassesProfilerPaths(t *testing.T) {
	sw := string(swJS)
	if !strings.Contains(sw, "startsWith('/_spx/')") {
		t.Error("sw.js fetch handler must bypass /_spx/ so the profiler UI is never served from the SW cache")
	}
	if !strings.Contains(sw, "startsWith('/api/')") {
		t.Error("sw.js must still bypass /api/")
	}
}
