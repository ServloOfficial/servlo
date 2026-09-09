package backup

import (
	"slices"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

func scheduledSite() config.Site {
	return config.Site{Name: "acme", Domains: []string{"acme-supply.com"}, Path: "/home/dev/code/acme", PHPVersion: "8.4"}
}

// The unit runs servlo's own binary rather than a shell pipeline, because a
// backup that depends on what is on PATH at 3am is a backup that stops working
// the first time PATH changes.
func TestScheduleUnits_RunServloItself(t *testing.T) {
	site := scheduledSite()
	service, err := ServiceUnit(site, "/home/dev/.local/bin/servlo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(service, "/home/dev/.local/bin/servlo backup acme") {
		t.Errorf("the unit does not run the backup:\n%s", service)
	}
	if !strings.Contains(service, "Type=oneshot") {
		t.Errorf("a backup is not a long-running service:\n%s", service)
	}
}

// Persistent, because the whole point of a nightly backup is the night the
// droplet was rebooted. Skipping that day silently is the failure.
func TestScheduleUnits_CatchUpAfterTheServerWasOff(t *testing.T) {
	timer, err := TimerUnit(scheduledSite(), "*-*-* 03:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(timer, "Persistent=true") {
		t.Errorf("a missed backup would be skipped rather than caught up:\n%s", timer)
	}
	if !strings.Contains(timer, "OnCalendar=*-*-* 03:30:00") {
		t.Errorf("the schedule did not reach the timer:\n%s", timer)
	}
}

// Every line of a unit file is a line of a unit file. A schedule or a site name
// carrying a newline would write whatever came after it into the unit.
func TestScheduleUnits_RefuseAnythingThatCouldWriteIntoTheUnit(t *testing.T) {
	if _, err := TimerUnit(scheduledSite(), "*-*-* 03:00:00\n[Service]\nExecStart=/bin/sh"); err == nil {
		t.Error("a schedule carrying a newline was accepted into a unit file")
	}
	bad := scheduledSite()
	bad.Name = "acme\nExecStart=/bin/sh"
	if _, err := ServiceUnit(bad, "/usr/bin/servlo"); err == nil {
		t.Error("a site name carrying a newline was accepted into a unit file")
	}
}

// A schedule is written the way the operator already knows one, and normalised
// on the way in, the same as a cron entry's.
func TestNormalizeScheduleForBackup(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// systemd understands "daily" itself, so it passes through rather than
		// being rewritten into a form that means the same thing.
		{"daily", "daily"},
		{"30 3 * * *", "*-*-* 03:30:00"},
		{"*-*-* 03:30:00", "*-*-* 03:30:00"},
	} {
		got, err := NormalizeSchedule(tc.in)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q became %q, want %q", tc.in, got, tc.want)
		}
	}
	if _, err := NormalizeSchedule("every so often"); err == nil {
		t.Error("a schedule nothing can parse was accepted")
	}
}

// recordingMgr is systemd, remembered rather than run.
type recordingMgr struct {
	services.ServiceManager
	calls  []string
	units  map[string]string
	timers map[string]string
}

func newRecordingMgr() *recordingMgr {
	return &recordingMgr{units: map[string]string{}, timers: map[string]string{}}
}

func (m *recordingMgr) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	m.units[name] = body
	m.calls = append(m.calls, "write-service:"+name)
	return true, nil
}
func (m *recordingMgr) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	m.timers[name] = body
	m.calls = append(m.calls, "write-timer:"+name)
	return true, nil
}
func (m *recordingMgr) RemoveServiceUnit(name string) error {
	m.calls = append(m.calls, "remove-service:"+name)
	return nil
}
func (m *recordingMgr) RemoveTimerUnit(name string) error {
	m.calls = append(m.calls, "remove-timer:"+name)
	return nil
}
func (m *recordingMgr) DaemonReload() error { m.calls = append(m.calls, "reload"); return nil }
func (m *recordingMgr) Enable(name string) error {
	m.calls = append(m.calls, "enable:"+name)
	return nil
}
func (m *recordingMgr) Disable(name string) error {
	m.calls = append(m.calls, "disable:"+name)
	return nil
}
func (m *recordingMgr) Start(name string) error { m.calls = append(m.calls, "start:"+name); return nil }
func (m *recordingMgr) Stop(name string) error  { m.calls = append(m.calls, "stop:"+name); return nil }

func swapMgr(t *testing.T, m services.ServiceManager) {
	t.Helper()
	prev := services.Mgr
	services.Mgr = m
	t.Cleanup(func() { services.Mgr = prev })
}

func TestApplySchedule_WritesThePairAndStartsTheTimer(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := scheduledSite()
	site.Backup = &config.SiteBackup{Schedule: "*-*-* 03:30:00"}

	if err := ApplySchedule(site, "/usr/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	unit := UnitName(site.Name)
	for _, want := range []string{"write-service:" + unit, "write-timer:" + unit, "reload", "enable:" + unit + ".timer", "start:" + unit + ".timer"} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("scheduling did not %s; calls were %v", want, mgr.calls)
		}
	}
}

// Switching a site's backups off for an afternoon keeps the schedule but stops
// the timer, so it does not have to be retyped afterwards.
func TestApplySchedule_ADisabledScheduleIsNotRunning(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := scheduledSite()
	site.Backup = &config.SiteBackup{Schedule: "*-*-* 03:30:00", Disabled: true}

	if err := ApplySchedule(site, "/usr/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	unit := UnitName(site.Name) + ".timer"
	if slices.Contains(mgr.calls, "start:"+unit) {
		t.Error("a disabled schedule started its timer")
	}
	for _, want := range []string{"stop:" + unit, "disable:" + unit} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("a disabled schedule did not %s; calls were %v", want, mgr.calls)
		}
	}
	if _, ok := mgr.timers[UnitName(site.Name)]; !ok {
		t.Error("the timer file was deleted rather than stopped, so the schedule is gone")
	}
}

// A site with no schedule at all has no units. Leaving them behind would keep
// backing up a site whose operator turned the schedule off.
func TestApplySchedule_NoScheduleTakesTheUnitsAway(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := scheduledSite()

	if err := ApplySchedule(site, "/usr/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	unit := UnitName(site.Name)
	for _, want := range []string{"stop:" + unit + ".timer", "remove-timer:" + unit, "remove-service:" + unit} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("an unscheduled site kept its units: did not %s; calls were %v", want, mgr.calls)
		}
	}
}

// The scheduled check runs the verify, not the backup. Getting that wrong
// would arm a second nightly backup and quietly double the disk.
func TestVerifyUnits_RunTheCheckWeekly(t *testing.T) {
	service, timer, err := VerifyUnits(scheduledSite(), "/usr/bin/servlo", "Sun *-*-* 04:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(service, "backup verify --latest acme") {
		t.Errorf("the unit does not run the check:\n%s", service)
	}
	if strings.Contains(service, "ExecStart=/usr/bin/servlo backup acme") {
		t.Errorf("the check unit takes a backup instead of verifying one:\n%s", service)
	}
	if !strings.Contains(timer, "OnCalendar=Sun *-*-* 04:00:00") {
		t.Errorf("the schedule did not reach the timer:\n%s", timer)
	}
}

// The two pairs must not share a name, or arming one would overwrite the other
// and a site would end up with a check and no backup, or the reverse.
func TestVerifyUnitName_IsNotTheBackupsOwn(t *testing.T) {
	site := scheduledSite()
	if VerifyUnitName(site.Name) == UnitName(site.Name) {
		t.Fatal("the check and the backup share a unit name, so one overwrites the other")
	}
}

func TestApplySchedule_ArmsTheCheckAlongsideTheBackup(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := scheduledSite()
	site.Backup = &config.SiteBackup{Schedule: "*-*-* 03:30:00", Verify: "Sun *-*-* 04:00:00"}

	if err := ApplySchedule(site, "/usr/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	verify := VerifyUnitName(site.Name)
	for _, want := range []string{"write-service:" + verify, "write-timer:" + verify, "start:" + verify + ".timer"} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("the scheduled check was not armed: no %s; calls were %v", want, mgr.calls)
		}
	}
}

// A site that schedules backups but no check has no check units. Leaving them
// behind would keep restoring a database every week for a site whose operator
// turned that off.
func TestApplySchedule_NoCheckTakesTheCheckUnitsAway(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := scheduledSite()
	site.Backup = &config.SiteBackup{Schedule: "*-*-* 03:30:00"}

	if err := ApplySchedule(site, "/usr/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	verify := VerifyUnitName(site.Name)
	if !slices.Contains(mgr.calls, "remove-timer:"+verify) {
		t.Errorf("a site with no scheduled check kept its check units; calls were %v", mgr.calls)
	}
	// And the backup itself is still armed.
	if !slices.Contains(mgr.calls, "start:"+UnitName(site.Name)+".timer") {
		t.Error("removing the check took the backup with it")
	}
}

// A backup timer is named for the site and nothing else, and unlink never
// removed it: it stayed armed, failing nightly against a site that was gone.
// Worse than the noise is what happens when the name comes back. Site handles
// are derived from directory names, so a second shop.com on this server picks
// up the first one's schedule without anybody setting one.
func TestRemoveSiteSchedules_TakesBothPairsOffTheMachine(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)

	if err := RemoveSiteSchedules("shop"); err != nil {
		t.Fatal(err)
	}

	for _, unit := range []string{"servlo-backup-shop", "servlo-backup-verify-shop"} {
		for _, call := range []string{"stop:" + unit + ".timer", "disable:" + unit + ".timer",
			"remove-timer:" + unit, "remove-service:" + unit} {
			if !slices.Contains(mgr.calls, call) {
				t.Errorf("%q never happened; calls were %v", call, mgr.calls)
			}
		}
	}
}
