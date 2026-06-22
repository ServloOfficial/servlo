package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

// Two failing workers on the same site must get distinct zone ids, otherwise
// bubblezone keeps a single region per id and the second row is unclickable.
func TestDashWorkersCard_DistinctZonesPerFailingWorker(t *testing.T) {
	m := NewModel("test")
	m.snap = Snapshot{
		Sites: []siteinfo.EnrichedSite{
			{Name: "alpha", QueueFailing: true, ScheduleFailing: true},
		},
	}
	c := m.dashWorkersCard(80)

	var failZones []string
	for _, id := range c.rowZones {
		if strings.HasPrefix(id, "dashfailsite:") {
			failZones = append(failZones, id)
		}
	}
	if len(failZones) != 2 {
		t.Fatalf("expected 2 failing-worker zones, got %d (%v)", len(failZones), failZones)
	}
	seen := map[string]bool{}
	for _, id := range failZones {
		if seen[id] {
			t.Fatalf("duplicate failing-worker zone id %q; second row would be a dead click", id)
		}
		seen[id] = true
	}
}

// Navigating to a site by name must clear a leftover filter that would hide the
// target, so a dashboard click can't silently fail to select.
func TestSelectSiteByName_ClearsHidingFilter(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap() // sites: alpha, beta
	m.siteFilter = "alpha"

	m.selectSiteByName("beta")

	if m.siteFilter != "" {
		t.Fatalf("filter %q hid the target; it should have been cleared", m.siteFilter)
	}
	vis := m.visibleSites()
	if m.siteCursor < 0 || m.siteCursor >= len(vis) || vis[m.siteCursor].Name != "beta" {
		t.Fatalf("cursor %d does not point at beta in %d visible sites", m.siteCursor, len(vis))
	}
}

func TestSelectServiceByName_ClearsHidingFilter(t *testing.T) {
	m := NewModel("test")
	m.snap = fakeSnap() // services: mysql, redis, mailpit
	m.svcFilter = "mysql"

	m.selectServiceByName("redis")

	if m.svcFilter != "" {
		t.Fatalf("filter %q hid the target; it should have been cleared", m.svcFilter)
	}
	vis := m.visibleServices()
	if m.svcCursor < 0 || m.svcCursor >= len(vis) || vis[m.svcCursor].Name != "redis" {
		t.Fatalf("cursor %d does not point at redis in %d visible services", m.svcCursor, len(vis))
	}
}
