package tui

import (
	"strings"
	"testing"

	"github.com/geodro/lerd/internal/siteinfo"
)

func dashNavModel() *Model {
	m := NewModel("test")
	m.width = 120
	m.height = 30
	m.activeTab = tabDashboard
	m.snap = Snapshot{
		Sites: []siteinfo.EnrichedSite{
			{Name: "alpha", Domains: []string{"alpha.test"}, FPMRunning: true},
			{Name: "beta", Domains: []string{"beta.test"}, Paused: true},
		},
		Services: []ServiceRow{
			{Name: "mysql", State: stateRunning},
			{Name: "redis", State: stateStopped},
			{Name: "queue-alpha", State: stateRunning, WorkerKind: "queue", WorkerSite: "alpha"},
		},
		Status: StatusRow{TLD: "test"},
	}
	// Render once so dashZones reflects what's on screen, like the real loop.
	_ = m.renderDashboardGrid(120, 24)
	return m
}

func TestDashNav_ArrowsMoveRowCursorAndEnterJumps(t *testing.T) {
	m := dashNavModel() // Sites card focused by default
	if got := len(m.dashZones[0]); got != 2 {
		t.Fatalf("expected 2 site zones, got %d", got)
	}
	m.moveCursor(1)
	if m.dashRowCursor[0] != 1 {
		t.Fatalf("down should move the row cursor to 1, got %d", m.dashRowCursor[0])
	}
	m.moveCursor(5)
	if m.dashRowCursor[0] != 1 {
		t.Fatalf("cursor should clamp at the last row (1), got %d", m.dashRowCursor[0])
	}
	m.activateDashSelection()
	if m.activeTab != tabSites {
		t.Fatalf("enter should jump to the Sites tab, got %d", m.activeTab)
	}
	if s := m.currentSite(); s == nil || s.Name != "beta" {
		t.Fatalf("enter should select the cursor's site (beta), got %v", s)
	}
}

func TestDashNav_InfoCardScrollsInsteadOfSelecting(t *testing.T) {
	m := dashNavModel()
	m.dashFocus = 5 // Lerd card: info only, no selectable rows
	if len(m.dashZones[5]) != 0 {
		t.Fatalf("info card should have no selectable zones")
	}
	m.moveCursor(1)
	if m.dashScroll[5] != 1 {
		t.Fatalf("an info card should scroll on ↑↓, got scroll %d", m.dashScroll[5])
	}
	if m.dashRowCursor[5] != 0 {
		t.Fatalf("an info card should not move a row cursor, got %d", m.dashRowCursor[5])
	}
}

func TestDashNav_ServicesEnterSelectsService(t *testing.T) {
	m := dashNavModel()
	m.dashFocus = 1 // Services card (mysql, redis; the worker is excluded)
	if len(m.dashZones[1]) != 2 {
		t.Fatalf("expected 2 service zones, got %d", len(m.dashZones[1]))
	}
	m.dashRowCursor[1] = 1 // redis
	m.activateDashSelection()
	if m.activeTab != tabServices {
		t.Fatalf("enter should jump to the Services tab, got %d", m.activeTab)
	}
	if s := m.currentService(); s == nil || s.Name != "redis" {
		t.Fatalf("enter should select redis, got %v", s)
	}
}

func TestDashNav_WorkerEnterJumpsToOwningService(t *testing.T) {
	m := dashNavModel()
	m.dashFocus = 2 // Workers card
	if len(m.dashZones[2]) != 1 {
		t.Fatalf("expected 1 worker zone, got %d", len(m.dashZones[2]))
	}
	m.activateDashSelection()
	if m.activeTab != tabServices {
		t.Fatalf("worker enter should jump to the Services tab, got %d", m.activeTab)
	}
	if s := m.currentService(); s == nil || s.Name != "queue-alpha" {
		t.Fatalf("worker enter should select queue-alpha, got %v", s)
	}
}

func TestDashNav_SelectedRowDrawsCaret(t *testing.T) {
	m := dashNavModel() // Sites card focused, row 0 selected
	out := stripANSI(m.renderDashboardGrid(120, 24))
	if !strings.Contains(out, "▸ ● alpha.test") {
		t.Fatalf("focused card's selected row should show the caret:\n%s", out)
	}
}
