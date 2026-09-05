package logrotate

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

// The timer. One for the server rather than one per site: rotation is cheap,
// the work is the same for every site, and a server with forty sites does not
// want forty timers in systemctl list-timers between its backups.

// UnitName is the unit pair that rotates the logs.
const UnitName = "servlo-logrotate"

// Calendar is when it runs. Daily and early, before the backup at 03:30, so a
// site's archive carries rotated logs rather than one enormous live one. The
// randomised delay keeps it off the same second as everything else.
const Calendar = "*-*-* 02:30:00"

// ServiceUnit is the oneshot that rotates.
//
// servlo by absolute path rather than a shell pipeline, for the same reason the
// backup timer does it: a unit that resolves servlo from PATH stops working the
// first time PATH changes, and the way that failure shows up is a disk that
// filled months later.
func ServiceUnit(servloBin string) (string, error) {
	if servloBin == "" {
		return "", fmt.Errorf("no servlo binary to run")
	}
	if config.ContainsUnitInjectionChars(servloBin) {
		return "", fmt.Errorf("a servlo path must not contain a newline or a NUL: every line of it is a line of a unit file")
	}
	return fmt.Sprintf(`[Unit]
Description=Servlo log rotation

[Service]
Type=oneshot
ExecStart=%s logs rotate
`, servloBin), nil
}

// TimerUnit is when.
//
// Persistent, because a droplet that was off overnight should rotate when it
// comes back rather than skip the day. A skipped rotation is not urgent on its
// own; a month of skipped ones is the disk alert.
func TimerUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Servlo log rotation

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=600
Unit=%s.service

[Install]
WantedBy=timers.target
`, Calendar, UnitName)
}

// ApplySchedule makes systemd match the setting.
//
// One entry point for both states, because the state that matters is the one on
// the machine. Rotation switched off removes the units rather than leaving a
// disabled pair behind for somebody to find and wonder about.
func ApplySchedule(enabled bool, servloBin string) error {
	timer := UnitName + ".timer"
	if !enabled {
		_ = services.Mgr.Stop(timer)
		_ = services.Mgr.Disable(timer)
		_ = services.Mgr.RemoveTimerUnit(UnitName)
		_ = services.Mgr.RemoveServiceUnit(UnitName)
		return services.Mgr.DaemonReload()
	}

	service, err := ServiceUnit(servloBin)
	if err != nil {
		return err
	}
	if _, err := services.Mgr.WriteServiceUnitIfChanged(UnitName, service); err != nil {
		return fmt.Errorf("writing %s.service: %w", UnitName, err)
	}
	if _, err := services.Mgr.WriteTimerUnitIfChanged(UnitName, TimerUnit()); err != nil {
		return fmt.Errorf("writing %s.timer: %w", UnitName, err)
	}
	// Unconditional: a rewritten unit systemd has not re-read is the old
	// schedule still armed.
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}
	if err := services.Mgr.Enable(timer); err != nil {
		return fmt.Errorf("enabling %s: %w", timer, err)
	}
	return services.Mgr.Start(timer)
}

// JournalCommands are what to run to bound the journal, which is where nginx,
// PHP-FPM and every worker actually log.
//
// Printed rather than run. journald's configuration is a system file and
// changing it needs root, which servlo does not have and does not ask for
// (CLAUDE.md §3.2). Reporting the limit honestly and handing over the exact
// command is the whole of what servlo can do here.
func JournalCommands(maxUse string) []string {
	return []string{
		fmt.Sprintf("sudo mkdir -p /etc/systemd/journald.conf.d && printf '[Journal]\\nSystemMaxUse=%s\\nMaxRetentionSec=1month\\n' | sudo tee /etc/systemd/journald.conf.d/servlo.conf", maxUse),
		"sudo systemctl restart systemd-journald",
	}
}
