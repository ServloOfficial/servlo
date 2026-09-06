package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ServloOfficial/servlo/internal/config"
	servloSystemd "github.com/ServloOfficial/servlo/internal/systemd"
)

// settingsRow describes one focusable line in the settings view.
type settingsRow struct {
	kind       settingsKind
	label      string
	on         bool
	phpVersion string // PHP version, for per-version rows
}

type settingsKind int

const (
	settingsAutostart settingsKind = iota
	settingsWorkerMode
)

func (m *Model) settingsRows() []settingsRow {
	var rows []settingsRow

	rows = append(rows, settingsRow{
		kind:  settingsAutostart,
		label: "Autostart servlo on login",
		on:    servloSystemd.IsAutostartEnabled(),
	})

	// Worker runtime mode: macOS only. On Linux workers always run via
	// podman exec under systemd so the setting is meaningless there and
	// is hidden from the UI.

	return rows
}

func (m *Model) settingsToggle(rows []settingsRow) tea.Cmd {
	if len(rows) == 0 {
		return nil
	}
	if m.settingsRow >= len(rows) {
		m.settingsRow = len(rows) - 1
	}
	row := rows[m.settingsRow]
	switch row.kind {
	case settingsAutostart:
		sub := "enable"
		if row.on {
			sub = "disable"
		}
		m.setStatus("autostart "+sub+"…", 5*time.Second)
		return runServlo("", "autostart", sub)
	case settingsWorkerMode:
		// Toggle between exec (off) and container (on). Mirrors
		// `servlo workers mode <value>`. Does not stop running workers —
		// caller should restart them for the change to take effect.
		target := config.WorkerExecModeContainer
		if row.on {
			target = config.WorkerExecModeExec
		}
		m.setStatus("switching worker mode to "+target+"…", 5*time.Second)
		return runServlo("", "workers", "mode", target)
	}
	return nil
}
