package sitecron

import (
	"fmt"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

// Apply writes one entry's unit pair and puts it in the state the entry asks
// for: scheduled, or kept but stopped.
//
// Both halves are written every time rather than only when something changed,
// because the manager already skips an identical write and the alternative is a
// second place that has to decide what "changed" means.
func Apply(site config.Site, e config.CronEntry) error {
	service, err := ServiceUnit(site, e)
	if err != nil {
		return err
	}
	timer, err := TimerUnit(site, e)
	if err != nil {
		return err
	}

	unit := UnitName(site.Name, e.ID)
	if _, err := services.Mgr.WriteServiceUnitIfChanged(unit, service); err != nil {
		return fmt.Errorf("writing %s.service: %w", unit, err)
	}
	if _, err := services.Mgr.WriteTimerUnitIfChanged(unit, timer); err != nil {
		return fmt.Errorf("writing %s.timer: %w", unit, err)
	}
	// Unconditional: a rewritten unit systemd has not re-read is the old one
	// still running, and this is cheap next to a command that runs every minute.
	if err := services.Mgr.DaemonReload(); err != nil {
		return fmt.Errorf("reloading systemd: %w", err)
	}

	timerUnit := unit + ".timer"
	if e.Disabled {
		// Stop first: disabling a running timer leaves it running until the
		// next boot, which is a job the operator believes they turned off.
		_ = services.Mgr.Stop(timerUnit)
		if err := services.Mgr.Disable(timerUnit); err != nil {
			return fmt.Errorf("disabling %s: %w", timerUnit, err)
		}
		return nil
	}
	if err := services.Mgr.Enable(timerUnit); err != nil {
		return fmt.Errorf("enabling %s: %w", timerUnit, err)
	}
	if err := services.Mgr.Start(timerUnit); err != nil {
		return fmt.Errorf("starting %s: %w", timerUnit, err)
	}
	return nil
}

// Remove takes an entry's schedule off the machine entirely.
//
// Every step runs whether or not the one before it worked, because the failure
// this guards against is a half-removed entry: a timer file with nothing in the
// panel beside it is a command still running that nobody can see or stop.
func Remove(siteName, id string) error {
	unit := UnitName(siteName, id)
	return removeUnit(unit)
}

func removeUnit(unit string) error {
	timerUnit := unit + ".timer"
	_ = services.Mgr.Stop(timerUnit)
	_ = services.Mgr.Disable(timerUnit)
	// The oneshot may be mid-run; stopping it is what keeps a delete from
	// leaving the command running after its timer is gone.
	_ = services.Mgr.Stop(unit)

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
		return fmt.Errorf("removing %s: %s", unit, strings.Join(failures, "; "))
	}
	return nil
}

// Stop takes every one of a site's timers off the machine without touching what
// the site says should be scheduled.
//
// This is what pausing needs. A paused site has its workers stopped and its
// vhost swapped for the paused page, and a timer still firing php artisan at it
// is the site running with the lights off: migrations, queue pruning and
// reconciliation against a site the operator believes is down.
//
// The unit files stay where they are. Resume has to put back exactly the
// schedule that was there, and the entry is the only record of what that was.
func Stop(site config.Site) error {
	var failures []string
	for _, e := range site.Cron {
		timerUnit := UnitName(site.Name, e.ID) + ".timer"
		// Stop before disable, the same order Apply uses: disabling a running
		// timer leaves it running until the next boot.
		_ = services.Mgr.Stop(timerUnit)
		if err := services.Mgr.Disable(timerUnit); err != nil {
			failures = append(failures, fmt.Sprintf("disabling %s: %v", timerUnit, err))
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// Resume puts a paused site's schedule back. An entry the operator had switched
// off stays off, because Apply reads Disabled and pausing never wrote to it.
func Resume(site config.Site) error {
	var failures []string
	for _, e := range site.Cron {
		if err := Apply(site, e); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// Sync makes the machine match the site: every entry it has, and no unit for an
// entry it no longer has.
//
// The second half is the point. A delete that wrote the registry and then
// failed at systemd, or an entry removed while servlo was not running, leaves a
// timer with nothing behind it, and the next save is the natural moment to find
// it.
func Sync(site config.Site) error {
	expected := map[string]bool{}
	var failures []string
	for _, e := range site.Cron {
		expected[UnitName(site.Name, e.ID)] = true
		if err := Apply(site, e); err != nil {
			failures = append(failures, err.Error())
		}
	}
	for _, unit := range strayUnits(site, expected) {
		if err := removeUnit(unit); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// RemoveAll takes every unit this site has off the machine, for a site that is
// going away. It sweeps by prefix rather than by the site's entries, because
// Sync is the only thing that finds a unit the registry has forgotten and Sync
// never runs for a site that is no longer registered: whatever is left here is
// left for good, firing a command at a directory nobody serves.
func RemoveAll(site config.Site) error {
	var failures []string
	for _, unit := range strayUnits(site, nil) {
		if err := removeUnit(unit); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

// strayUnits are this site's cron units with no entry behind them.
//
// A prefix match alone would take another site's units with it: "acme" is a
// prefix of "acme-two", so servlo-cron-acme-two-report reads as an entry called
// "two-report" on acme. The registry is what settles it, the same way the
// orphaned-worker scan does.
func strayUnits(site config.Site, expected map[string]bool) []string {
	prefix := UnitPrefixFor(site.Name)
	var longer []string
	if reg, err := config.LoadSites(); err == nil {
		for _, s := range reg.Sites {
			if len(s.Name) > len(site.Name) {
				longer = append(longer, UnitPrefixFor(s.Name))
			}
		}
	}

	var stray []string
	for _, unit := range services.Mgr.ListServiceUnits(prefix + "*") {
		unit = strings.TrimSuffix(unit, ".service")
		if expected[unit] || !strings.HasPrefix(unit, prefix) {
			continue
		}
		claimed := false
		for _, other := range longer {
			if strings.HasPrefix(unit, other) {
				claimed = true
				break
			}
		}
		if !claimed {
			stray = append(stray, unit)
		}
	}
	return stray
}
