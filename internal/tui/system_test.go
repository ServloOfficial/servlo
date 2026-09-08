package tui

import (
	"strings"
	"testing"
)

// TestSystemRows_ContainsCoreSections checks every section header the system
// page promises (Nginx, Watcher, Notifications, PHP, Node,
// Servlo) is rendered. Worker mode is platform-gated and tested separately.
func TestSystemRows_ContainsCoreSections(t *testing.T) {
	m := NewModel("test")
	rows := m.systemRows()

	want := []string{"Nginx", "Watcher", "Notifications", "PHP versions", "Node", "Servlo"}
	have := map[string]bool{}
	for _, r := range rows {
		if r.kind == sysHeader {
			have[r.label] = true
		}
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("missing section header %q in system rows", w)
		}
	}
}

// TestNavigableSystemRows_SkipsHeadersAndInfo verifies the cursor only lands
// on interactive rows; header and info rows are scenery.
func TestNavigableSystemRows_SkipsHeadersAndInfo(t *testing.T) {
	rows := []systemRow{
		{kind: sysHeader, label: "X"},
		{kind: sysInfo, label: "a"},
		{kind: sysInfo, label: "b"},
		{kind: sysNotifEnabled, label: "Notif"},
		{kind: sysHeader, label: "Y"},
		{kind: sysAutostart, label: "Auto"},
	}
	nav := navigableSystemRows(rows)
	if len(nav) != 2 {
		t.Fatalf("expected 2 navigable rows, got %d", len(nav))
	}
	for _, idx := range nav {
		kind := rows[idx].kind
		if kind == sysHeader || kind == sysInfo {
			t.Errorf("navigable index %d points to non-interactive kind %v", idx, kind)
		}
	}
}

// TestSystemContentLines_RendersHeader checks the rendered output contains
// the System title and the return-key hint so users discover how to leave.
func TestSystemContentLines_RendersHeader(t *testing.T) {
	m := NewModel("test")
	lines, _ := systemContentLinesWithCursor(m, false, 100)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "System") {
		t.Errorf("system page should render title:\n%s", joined)
	}
	if !strings.Contains(joined, "Y or esc") {
		t.Errorf("system page should hint at the return key:\n%s", joined)
	}
}

// TestSystemContentLines_CursorLineLandsOnInteractiveRow ensures the
// reported cursor row index actually corresponds to a toggleable row in
// the output, so the viewport will keep the selection visible.
func TestSystemContentLines_CursorLineLandsOnInteractiveRow(t *testing.T) {
	m := NewModel("test")
	m.systemRow = 0
	lines, cursorLine := systemContentLinesWithCursor(m, true, 100)
	if cursorLine == 0 {
		// 0 means "no interactive row rendered" — the page must always have
		// at least the notifications toggle, so this is a regression.
		t.Fatal("expected cursorLine > 0 when at least one interactive row exists")
	}
	if cursorLine >= len(lines) {
		t.Fatalf("cursorLine %d out of bounds (%d lines)", cursorLine, len(lines))
	}
	// The selected line should carry the inverted accent prefix used by
	// renderDetailRow — "▸" is the universal marker for the focused row.
	if !strings.Contains(lines[cursorLine], "▸") {
		t.Errorf("cursor line %q lacks the ▸ marker", lines[cursorLine])
	}
}
