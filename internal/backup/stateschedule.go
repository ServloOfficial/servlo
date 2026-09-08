package backup

import (
	"fmt"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

// The server's own nightly backup.
//
// One timer for the machine rather than one per site, the same shape as log
// rotation, because there is one server state and taking it is cheap.
//
// It exists because nothing scheduled it. Every site could be on a nightly
// timer with archives going offsite, and the archive that makes any of them
// restorable was written only when somebody remembered to type the command. A
// rebuild then restores files and databases onto a server holding whatever the
// registry looked like the last time a human thought about it, which on most
// machines is the day it was set up.

// StateUnitName is the unit pair that backs up the server's own state.
const StateUnitName = "servlo-backup-state"

// StateCalendar is when it runs. Before the site backups at 03:30 rather than
// after: the state archive is small and quick, and taking it first means a long
// site backup cannot push it into the morning.
const StateCalendar = "*-*-* 03:00:00"

// StateServiceUnit is the oneshot that takes it.
//
// servlo by absolute path, for the reason every other unit here does it: a unit
// that resolves servlo from PATH stops working the first time PATH changes, and
// the way that failure surfaces is a rebuild that has nothing to rebuild from.
func StateServiceUnit(servloBin string) (string, error) {
	if servloBin == "" {
		return "", fmt.Errorf("no servlo binary to run")
	}
	if config.ContainsUnitInjectionChars(servloBin) {
		return "", fmt.Errorf("a servlo path must not contain a newline or a NUL: every line of it is a line of a unit file")
	}
	return fmt.Sprintf(`[Unit]
Description=Servlo backup of the server's own state

[Service]
Type=oneshot
ExecStart=%s backup state
`, servloBin), nil
}

// StateTimerUnit is when.
//
// Persistent, because the night the droplet was rebooted is exactly the night
// worth having. A timer that silently skips it leaves the newest state archive
// a day older than every site archive beside it.
func StateTimerUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Servlo backup of the server's own state

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=300
Unit=%s.service

[Install]
WantedBy=timers.target
`, StateCalendar, StateUnitName)
}

// ApplyStateSchedule makes systemd match the setting.
//
// Not per site: there is one server state, and a machine backing up its sites
// nightly while its own registry ages is the failure this closes. Every install
// gets it, the way every install gets log rotation, because an operator cannot
// ask for a rebuild to be possible on the day they need it. The disabled branch
// exists so the units can be taken away cleanly rather than left disabled for
// somebody to find and wonder about.
func ApplyStateSchedule(enabled bool, servloBin string) error {
	timer := StateUnitName + ".timer"
	if !enabled {
		_ = services.Mgr.Stop(timer)
		_ = services.Mgr.Disable(timer)
		_ = services.Mgr.RemoveTimerUnit(StateUnitName)
		_ = services.Mgr.RemoveServiceUnit(StateUnitName)
		return services.Mgr.DaemonReload()
	}

	service, err := StateServiceUnit(servloBin)
	if err != nil {
		return err
	}
	if _, err := services.Mgr.WriteServiceUnitIfChanged(StateUnitName, service); err != nil {
		return fmt.Errorf("writing %s.service: %w", StateUnitName, err)
	}
	if _, err := services.Mgr.WriteTimerUnitIfChanged(StateUnitName, StateTimerUnit()); err != nil {
		return fmt.Errorf("writing %s.timer: %w", StateUnitName, err)
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
