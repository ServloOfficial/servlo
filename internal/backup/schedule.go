package backup

import (
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
	"github.com/ServloOfficial/servlo/internal/sitecron"
)

// unitPrefix is what every backup unit is called, so a stale one left by a site
// that was removed is findable the same way a stale cron unit is.
const unitPrefix = "servlo-backup-"

// UnitName is the unit pair for a site's scheduled backup.
func UnitName(siteName string) string { return unitPrefix + siteName }

// NormalizeSchedule accepts a schedule written the way the operator already
// knows one and returns systemd's spelling.
//
// It is deliberately the same parser scheduled commands use. An operator who
// has typed a crontab line into the cron tab should not discover that the
// backup field wants something else.
func NormalizeSchedule(in string) (string, error) { return sitecron.NormalizeSchedule(in) }

// ServiceUnit is the oneshot that takes the backup.
//
// It runs servlo's own binary by absolute path rather than a shell pipeline: a
// backup that depends on what is on PATH at three in the morning is a backup
// that stops working the first time PATH changes, and nobody finds out until
// they need it.
func ServiceUnit(site config.Site, servloBin string) (string, error) {
	if err := validUnitValue("site name", site.Name); err != nil {
		return "", err
	}
	if err := validUnitValue("servlo path", servloBin); err != nil {
		return "", err
	}
	return fmt.Sprintf(`[Unit]
Description=Servlo backup of %s

[Service]
Type=oneshot
ExecStart=%s backup %s
`, site.Name, servloBin, site.Name), nil
}

// TimerUnit is when it runs.
//
// Persistent, because the whole point of a nightly backup is the night the
// droplet was rebooted. A timer that silently skips that day is a gap nobody
// sees until the restore.
func TimerUnit(site config.Site, calendar string) (string, error) {
	if err := validUnitValue("site name", site.Name); err != nil {
		return "", err
	}
	if err := validUnitValue("schedule", calendar); err != nil {
		return "", err
	}
	return fmt.Sprintf(`[Unit]
Description=Servlo backup of %s

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=300
Unit=%s.service

[Install]
WantedBy=timers.target
`, site.Name, calendar, UnitName(site.Name)), nil
}

// validUnitValue refuses what must never reach a unit file. Every line of one
// is a line of a unit file, so a value carrying a newline writes whatever comes
// after it into the unit as another directive.
func validUnitValue(what, value string) error {
	if value == "" {
		return fmt.Errorf("a scheduled backup needs a %s", what)
	}
	if config.ContainsUnitInjectionChars(value) {
		return fmt.Errorf("a %s must not contain a newline or a NUL: every line of it is a line of a unit file", what)
	}
	return nil
}

// ApplySchedule makes systemd match what the site says.
//
// One entry point for all three states rather than separate add and remove
// paths, because the state that matters is the one on the machine and there is
// exactly one place that decides it. A site with no schedule loses its units:
// leaving them behind would keep backing up a site whose operator switched the
// schedule off.
func ApplySchedule(site config.Site, servloBin string) error {
	unit := UnitName(site.Name)
	timer := unit + ".timer"

	if site.Backup == nil || site.Backup.Schedule == "" {
		// The check goes with it: a site not being backed up has nothing to
		// test-restore, and a check left armed would keep restoring last
		// month's archive every week for no reason.
		if err := applyVerifySchedule(site, servloBin); err != nil {
			return err
		}
		return removeSchedule(unit, timer)
	}

	service, err := ServiceUnit(site, servloBin)
	if err != nil {
		return err
	}
	timerBody, err := TimerUnit(site, site.Backup.Schedule)
	if err != nil {
		return err
	}
	if _, err := services.Mgr.WriteServiceUnitIfChanged(unit, service); err != nil {
		return fmt.Errorf("writing %s.service: %w", unit, err)
	}
	if _, err := services.Mgr.WriteTimerUnitIfChanged(unit, timerBody); err != nil {
		return fmt.Errorf("writing %s.timer: %w", unit, err)
	}
	// Unconditional: a rewritten unit systemd has not re-read is the old
	// schedule still armed.
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}

	if site.Backup.Disabled {
		// Stop before disable, because disabling a running timer leaves it
		// running until the next boot: a backup the operator believes is off.
		_ = services.Mgr.Stop(timer)
		if err := services.Mgr.Disable(timer); err != nil {
			return fmt.Errorf("disabling %s: %w", timer, err)
		}
		return nil
	}
	if err := services.Mgr.Enable(timer); err != nil {
		return fmt.Errorf("enabling %s: %w", timer, err)
	}
	if err := services.Mgr.Start(timer); err != nil {
		return fmt.Errorf("starting %s: %w", timer, err)
	}
	return applyVerifySchedule(site, servloBin)
}

// applyVerifySchedule does the same for the scheduled test restore, which is
// its own pair so switching one off leaves the other alone and systemctl shows
// which of the two is failing.
func applyVerifySchedule(site config.Site, servloBin string) error {
	unit := VerifyUnitName(site.Name)
	timer := unit + ".timer"

	if site.Backup == nil || site.Backup.Verify == "" || site.Backup.Schedule == "" {
		return removeSchedule(unit, timer)
	}

	service, timerBody, err := VerifyUnits(site, servloBin, site.Backup.Verify)
	if err != nil {
		return err
	}
	if _, err := services.Mgr.WriteServiceUnitIfChanged(unit, service); err != nil {
		return fmt.Errorf("writing %s.service: %w", unit, err)
	}
	if _, err := services.Mgr.WriteTimerUnitIfChanged(unit, timerBody); err != nil {
		return fmt.Errorf("writing %s.timer: %w", unit, err)
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}
	if site.Backup.Disabled {
		_ = services.Mgr.Stop(timer)
		return services.Mgr.Disable(timer)
	}
	if err := services.Mgr.Enable(timer); err != nil {
		return fmt.Errorf("enabling %s: %w", timer, err)
	}
	return services.Mgr.Start(timer)
}

// removeSchedule takes the pair off the machine. Every step runs whether or not
// the one before it worked: the failure this guards against is a half-removed
// schedule, which is a timer still firing with nothing in the panel beside it.
func removeSchedule(unit, timer string) error {
	_ = services.Mgr.Stop(timer)
	_ = services.Mgr.Disable(timer)

	var failures []string
	if err := services.Mgr.RemoveTimerUnit(unit); err != nil {
		failures = append(failures, err.Error())
	}
	if err := services.Mgr.RemoveServiceUnit(unit); err != nil {
		failures = append(failures, err.Error())
	}
	if err := services.Mgr.DaemonReload(); err != nil {
		failures = append(failures, err.Error())
	}
	if len(failures) > 0 {
		return fmt.Errorf("removing the backup schedule for %s: %s", unit, strings.Join(failures, "; "))
	}
	return nil
}

// VerifyUnitName is the pair for a site's scheduled test restore. Separate from
// the backup's own units so switching one off does not switch off the other,
// and so systemctl shows which of the two is failing.
func VerifyUnitName(siteName string) string { return unitPrefix + "verify-" + siteName }

// VerifyUnits are the oneshot and timer that check the newest archive.
//
// Weekly rather than nightly, and deliberately not part of the backup itself. A
// verify restores the whole database into a scratch copy, which on a large site
// is real work, and doing it after every backup would double the nightly cost
// to answer a question whose answer changes rarely.
func VerifyUnits(site config.Site, servloBin, calendar string) (string, string, error) {
	if err := validUnitValue("site name", site.Name); err != nil {
		return "", "", err
	}
	if err := validUnitValue("servlo path", servloBin); err != nil {
		return "", "", err
	}
	if err := validUnitValue("schedule", calendar); err != nil {
		return "", "", err
	}
	service := fmt.Sprintf(`[Unit]
Description=Servlo test restore of %s's newest backup

[Service]
Type=oneshot
ExecStart=%s backup verify --latest %s
`, site.Name, servloBin, site.Name)

	timer := fmt.Sprintf(`[Unit]
Description=Servlo test restore of %s's newest backup

[Timer]
OnCalendar=%s
Persistent=true
RandomizedDelaySec=900
Unit=%s.service

[Install]
WantedBy=timers.target
`, site.Name, calendar, VerifyUnitName(site.Name))
	return service, timer, nil
}
