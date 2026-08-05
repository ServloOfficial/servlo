package cli

import "testing"

// TestWorkerNameForSiteUnit covers the parsing the prune sweep relies on to
// decide which units belong to a site, including the prefix-collision case
// (site "app" vs "app-x") that the longest-match guard in stopAllSiteWorkerUnits
// uses to avoid tearing down another site's units.
func TestWorkerNameForSiteUnit(t *testing.T) {
	cases := []struct {
		unit, site string
		wantWorker string
		wantOK     bool
	}{
		{"servlo-queue-app", "app", "queue", true},               // parent unit
		{"servlo-vite-app-feat", "app", "vite", true},            // suffixed unit
		{"servlo-queue-other", "app", "", false},                 // different site
		{"servlo-vite-app-x", "app-x", "vite", true},             // parent unit of the longer-named site
		{"servlo-vite-app-x", "app", "vite", true},               // also matches "app" with a trailing token
		{"servlo-app", "app", "", false},                         // no worker segment
		{"queue-app", "app", "", false},                          // missing servlo- prefix
		{"servlo-messenger-my-app", "my-app", "messenger", true}, // hyphenated site name
	}
	for _, c := range cases {
		gotWorker, gotOK := workerNameForSiteUnit(c.unit, c.site)
		if gotOK != c.wantOK || gotWorker != c.wantWorker {
			t.Errorf("workerNameForSiteUnit(%q, %q) = (%q, %v), want (%q, %v)",
				c.unit, c.site, gotWorker, gotOK, c.wantWorker, c.wantOK)
		}
	}
}

// TestSiteOwnsWorkerUnit_declinesAmbiguous guards against tearing down another
// site's unit: unlinking a site named "feat" must NOT claim site "web"'s
// "servlo-horizon-web-feat" unit, because "web" is also a registered site.
func TestSiteOwnsWorkerUnit_declinesAmbiguous(t *testing.T) {
	// "feat" parses servlo-horizon-web-feat as its own (worker "horizon-web"), but
	// "web" is a registered site that also parses it, so ownership is declined.
	if _, ok := siteOwnsWorkerUnit("servlo-horizon-web-feat", "feat", []string{"web"}); ok {
		t.Error("site \"feat\" must not claim web's unit servlo-horizon-web-feat")
	}
	// With no colliding site registered, "feat" legitimately owns its own units.
	if _, ok := siteOwnsWorkerUnit("servlo-queue-feat", "feat", []string{"web"}); !ok {
		t.Error("site \"feat\" should own its own parent unit servlo-queue-feat")
	}
	// A site cleanly owns its unit when no other site collides.
	if _, ok := siteOwnsWorkerUnit("servlo-vite-app-feat", "app", []string{"web"}); !ok {
		t.Error("site \"app\" should own its unit servlo-vite-app-feat")
	}
}
