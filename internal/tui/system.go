package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ServloOfficial/servlo/internal/config"
	servloNode "github.com/ServloOfficial/servlo/internal/node"
	phpPkg "github.com/ServloOfficial/servlo/internal/php"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
	servloUpdate "github.com/ServloOfficial/servlo/internal/update"
)

// systemKind identifies each row in the System detail mode. Non-actionable
// rows use sysHeader / sysInfo so cursor navigation skips them.
type systemKind int

const (
	sysHeader systemKind = iota
	sysInfo
	sysNotifEnabled
	sysAutostart
)

// systemRow is one line in the System detail view. value is shown dimmed on
// the right of label; on drives the on/off glyph for toggle rows. arg holds
// per-row context (the PHP version, for per-version rows).
type systemRow struct {
	kind  systemKind
	label string
	value string
	on    bool
	arg   string
}

// systemRows produces every row the System detail view renders. Built per
// frame so live state (DNS status, container running, buffered dump count)
// stays in sync without a separate refresh path.
func (m *Model) systemRows() []systemRow {
	cfg, _ := config.LoadGlobal()
	rows := make([]systemRow, 0, 64)

	add := func(r systemRow) { rows = append(rows, r) }
	header := func(s string) { add(systemRow{kind: sysHeader, label: s}) }
	info := func(label, value string) { add(systemRow{kind: sysInfo, label: label, value: value}) }

	// Nginx
	header("Nginx")
	info("Status", runningOrStopped(m.snap.Status.NginxRunning))

	// Watcher
	header("Watcher")
	info("Status", runningOrStopped(m.snap.Status.WatcherRunning))

	// Notifications
	header("Notifications")
	notifOn := cfg != nil && cfg.IsNotificationsEnabled()
	add(systemRow{kind: sysNotifEnabled, label: "Enabled", on: notifOn})

	// PHP versions
	header("PHP versions")
	defaultPHP := ""
	if cfg != nil {
		defaultPHP = cfg.PHP.DefaultVersion
	}
	if defaultPHP != "" {
		info("Default", defaultPHP)
	}
	versions, _ := phpPkg.ListInstalled()
	if len(versions) == 0 {
		info("Installed", "none")
	}
	runningSet := map[string]bool{}
	for _, v := range m.snap.Status.PHPRunning {
		runningSet[v] = true
	}
	for _, v := range versions {
		state := "FPM stopped"
		if runningSet[v] {
			state = "FPM running"
		}
		if v == defaultPHP {
			state += " · default"
		}
		info("PHP "+v, state)
		// What this version's image carries of the custom extension/package
		// set. Omitted entirely when nothing is declared, so the row only
		// appears for the users it means something to.
		if extras := phpExtrasSummary(cfg, v); extras != "" {
			info("Extras · PHP "+v, extras)
		}
	}

	// Node
	header("Node")
	defaultNode := ""
	if cfg != nil {
		defaultNode = cfg.Node.DefaultVersion
	}
	if defaultNode != "" {
		info("Default", defaultNode)
	} else {
		info("Default", "(none)")
	}
	if nodeVersions := servloNode.ListInstalled(); len(nodeVersions) > 0 {
		info("Installed", strings.Join(nodeVersions, ", "))
	} else {
		info("Installed", "none (run `servlo node:install <ver>`)")
	}

	// Servlo
	header("Servlo")
	info("Version", m.version)
	if m.updateAvailable != "" {
		info("Update", m.updateAvailable+" available (run `servlo update`)")
	} else {
		if latest, _ := servloUpdate.CachedUpdateCheck(m.version); latest != nil && latest.LatestVersion != "" {
			info("Update", "you are on the latest ("+latest.LatestVersion+")")
		} else {
			info("Update", "no cached check yet")
		}
	}
	add(systemRow{kind: sysAutostart, label: "Autostart on login", on: servloSystemd.IsAutostartEnabled()})

	return rows
}

// navigableSystemRows returns the indices of focusable rows, skipping
// section headers and info-only rows. Lets the cursor land only on
// interactive items.
func navigableSystemRows(rows []systemRow) []int {
	out := make([]int, 0, len(rows))
	for i, r := range rows {
		if r.kind == sysHeader || r.kind == sysInfo {
			continue
		}
		out = append(out, i)
	}
	return out
}

// systemToggle dispatches the action for the focused row. Mirrors
// settingsToggle's shape: every branch shells out to the public CLI so the
// TUI shares the same code paths as a manual `servlo ...` invocation.
func (m *Model) systemToggle(rows []systemRow) tea.Cmd {
	nav := navigableSystemRows(rows)
	if len(nav) == 0 {
		return nil
	}
	if m.systemRow >= len(nav) {
		m.systemRow = len(nav) - 1
	}
	row := rows[nav[m.systemRow]]
	switch row.kind {
	case sysNotifEnabled:
		verb := "on"
		if row.on {
			verb = "off"
		}
		m.setStatus("notifications "+verb+"…", 5*time.Second)
		return runServlo("", "notify", verb)
	case sysAutostart:
		sub := "enable"
		if row.on {
			sub = "disable"
		}
		m.setStatus("autostart "+sub+"…", 5*time.Second)
		return runServlo("", "autostart", sub)
	}
	return nil
}

// systemContentLinesWithCursor renders the System detail pane and reports the
// rendered-line index of the currently-selected row so the viewport can keep
// the cursor on screen. Headers are bold, info rows are dim, toggleable rows
// show an on/off glyph plus state text. Mirrors settingsContentLines' clip/
// pad convention so the border wraps cleanly at the right edge.
func systemContentLinesWithCursor(m *Model, focused bool, innerW int) ([]string, int) {
	rows := m.systemRows()
	nav := navigableSystemRows(rows)
	navPos := func(i int) int {
		for pos, idx := range nav {
			if idx == i {
				return pos
			}
		}
		return -1
	}

	out := make([]string, 0, len(rows)+4)
	cursorLine := 0
	add := func(s string) { out = append(out, padToWidth(clipLine(s, innerW), innerW)) }

	add(sectionStyle.Render("System"))
	add(dimStyle.Render("  press Y or esc to return to site detail"))
	add("")

	for i, row := range rows {
		switch row.kind {
		case sysHeader:
			add("")
			add(sectionStyle.Render(row.label))
		case sysInfo:
			add(renderSystemInfoRow(row.label, row.value))
		default:
			selected := focused && navPos(i) == m.systemRow
			if selected {
				cursorLine = len(out)
			}
			add(renderDetailRow(selected, onOffGlyph(row.on), row.label, onOffText(row.on)))
		}
	}
	return out, cursorLine
}

// renderSystemInfoRow formats a left-label / right-value line. Keeps padding
// consistent with toggle rows so the columns line up.
func renderSystemInfoRow(label, value string) string {
	padded := label
	if w := len([]rune(label)); w < 18 {
		padded = label + spaces(18-w)
	}
	return "    " + dimStyle.Render(padded) + " " + value
}

func runningOrStopped(running bool) string {
	if running {
		return "running"
	}
	return "stopped"
}
