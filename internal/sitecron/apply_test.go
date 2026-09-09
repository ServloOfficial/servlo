package sitecron

import (
	"slices"
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/services"
)

// recordingMgr is systemd, remembered rather than run: every unit written and
// every lifecycle call, in order, so a test can assert that deleting an entry
// really does take both halves of the unit pair with it.
type recordingMgr struct {
	calls    []string
	units    map[string]string
	timers   map[string]string
	existing []string
}

func newRecordingMgr() *recordingMgr {
	return &recordingMgr{units: map[string]string{}, timers: map[string]string{}}
}

func (m *recordingMgr) WriteServiceUnit(name, body string) error {
	m.units[name] = body
	return nil
}
func (m *recordingMgr) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	changed := m.units[name] != body
	m.units[name] = body
	m.calls = append(m.calls, "write-service:"+name)
	return changed, nil
}
func (m *recordingMgr) RemoveServiceUnit(name string) error {
	delete(m.units, name)
	m.calls = append(m.calls, "remove-service:"+name)
	return nil
}
func (m *recordingMgr) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	changed := m.timers[name] != body
	m.timers[name] = body
	m.calls = append(m.calls, "write-timer:"+name)
	return changed, nil
}
func (m *recordingMgr) RemoveTimerUnit(name string) error {
	delete(m.timers, name)
	m.calls = append(m.calls, "remove-timer:"+name)
	return nil
}
func (m *recordingMgr) ListTimerUnits(glob string) []string {
	var out []string
	prefix := strings.TrimSuffix(glob, "*")
	for _, u := range m.existing {
		if strings.HasPrefix(u, prefix) {
			out = append(out, u+".timer")
		}
	}
	return out
}
func (m *recordingMgr) ListServiceUnits(glob string) []string {
	var out []string
	prefix := strings.TrimSuffix(glob, "*")
	for _, u := range m.existing {
		if strings.HasPrefix(u, prefix) {
			out = append(out, u)
		}
	}
	return out
}
func (m *recordingMgr) WriteContainerUnit(string, string) error { return nil }
func (m *recordingMgr) ContainerUnitInstalled(string) bool      { return false }
func (m *recordingMgr) RemoveContainerUnit(string) error        { return nil }
func (m *recordingMgr) ListContainerUnits(string) []string      { return nil }
func (m *recordingMgr) DaemonReload() error {
	m.calls = append(m.calls, "reload")
	return nil
}
func (m *recordingMgr) Start(name string) error   { m.calls = append(m.calls, "start:"+name); return nil }
func (m *recordingMgr) Stop(name string) error    { m.calls = append(m.calls, "stop:"+name); return nil }
func (m *recordingMgr) Restart(name string) error { return nil }
func (m *recordingMgr) Enable(name string) error {
	m.calls = append(m.calls, "enable:"+name)
	return nil
}
func (m *recordingMgr) Disable(name string) error {
	m.calls = append(m.calls, "disable:"+name)
	return nil
}
func (m *recordingMgr) IsActive(string) bool              { return false }
func (m *recordingMgr) IsEnabled(string) bool             { return false }
func (m *recordingMgr) UnitStatus(string) (string, error) { return "", nil }

func swapMgr(t *testing.T, m services.ServiceManager) {
	t.Helper()
	prev := services.Mgr
	services.Mgr = m
	t.Cleanup(func() { services.Mgr = prev })
}

func TestApply_WritesBothHalvesAndStartsTheTimer(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	entry := config.CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily", CaptureOutput: true}

	if err := Apply(site, entry); err != nil {
		t.Fatal(err)
	}

	unit := UnitName(site.Name, entry.ID)
	if _, ok := mgr.units[unit]; !ok {
		t.Error("no service unit was written")
	}
	if _, ok := mgr.timers[unit]; !ok {
		t.Error("no timer unit was written")
	}
	for _, want := range []string{"reload", "enable:" + unit + ".timer", "start:" + unit + ".timer"} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("Apply did not %s; calls were %v", want, mgr.calls)
		}
	}
}

// A disabled entry keeps its unit files, so turning it back on does not have to
// retype it, but nothing about it may still be scheduled.
func TestApply_ADisabledEntryIsNotScheduled(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	entry := config.CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily", Disabled: true}

	if err := Apply(site, entry); err != nil {
		t.Fatal(err)
	}

	unit := UnitName(site.Name, entry.ID)
	if slices.Contains(mgr.calls, "start:"+unit+".timer") {
		t.Error("a disabled entry started its timer")
	}
	for _, want := range []string{"stop:" + unit + ".timer", "disable:" + unit + ".timer"} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("a disabled entry did not %s; calls were %v", want, mgr.calls)
		}
	}
}

// The one that matters: a deleted entry must leave nothing behind. A timer file
// with no entry beside it is a command that keeps running with nowhere in the
// panel to see it or stop it.
func TestRemove_LeavesNoUnitBehind(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	entry := config.CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}
	if err := Apply(site, entry); err != nil {
		t.Fatal(err)
	}

	if err := Remove(site.Name, entry.ID); err != nil {
		t.Fatal(err)
	}

	unit := UnitName(site.Name, entry.ID)
	if _, ok := mgr.units[unit]; ok {
		t.Error("the service unit file survived the delete")
	}
	if _, ok := mgr.timers[unit]; ok {
		t.Error("the timer unit file survived the delete")
	}
	for _, want := range []string{
		"stop:" + unit + ".timer", "disable:" + unit + ".timer", "stop:" + unit,
		"remove-timer:" + unit, "remove-service:" + unit, "reload",
	} {
		if !slices.Contains(mgr.calls, want) {
			t.Errorf("Remove did not %s; calls were %v", want, mgr.calls)
		}
	}
}

// Sync is what runs after a save: whatever the site now says, and nothing else.
// A unit belonging to an entry that is gone is removed even though nobody asked
// for it by name, which is how an interrupted delete gets cleaned up.
func TestSync_RemovesUnitsForEntriesThatAreGone(t *testing.T) {
	// A second registered site whose name starts with this one's, so the prune
	// has to consult the registry rather than trust the prefix.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	for _, name := range []string{"acme", "acme-two"} {
		if err := config.AddSite(config.Site{Name: name, Domains: []string{name + ".example"}, Path: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newRecordingMgr()
	mgr.existing = []string{
		"servlo-cron-acme-prune",
		"servlo-cron-acme-stale",
		// A different site's entry, and a worker of this one. Neither is Sync's
		// to remove, and a prefix match that is not careful would take both.
		"servlo-cron-acme-two-report",
		"servlo-queue-acme",
	}
	swapMgr(t, mgr)

	site := testSite()
	site.Cron = []config.CronEntry{{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}}

	if err := Sync(site); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(mgr.calls, "remove-service:servlo-cron-acme-stale") {
		t.Errorf("the stale entry's unit was left behind; calls were %v", mgr.calls)
	}
	for _, mustKeep := range []string{"servlo-cron-acme-two-report", "servlo-queue-acme"} {
		if slices.Contains(mgr.calls, "remove-service:"+mustKeep) {
			t.Errorf("Sync removed %s, which is not this site's entry", mustKeep)
		}
	}
	if _, ok := mgr.units["servlo-cron-acme-prune"]; !ok {
		t.Error("Sync did not write the entry the site still has")
	}
}

// A site that cannot run a scheduled command at all still has to be syncable:
// pausing or converting a site must not fail on its cron.
func TestSync_ASiteWithNoContainerReportsTheProblem(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	site.HostPort = 3000
	site.Cron = []config.CronEntry{{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}}

	if err := Sync(site); err == nil {
		t.Fatal("a site with no container synced its cron without complaint")
	}
}

// A paused site is a site the operator took offline. Its workers stop and its
// vhost goes to the paused page, so its scheduled commands must stop too: a
// timer still firing php artisan against a site nobody can reach is the site
// running with the lights off.
func TestStop_TakesEveryTimerOffTheMachine(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	site.Cron = []config.CronEntry{
		{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"},
		{ID: "off", Name: "Off", Command: "php artisan off", Calendar: "daily", Disabled: true},
	}

	if err := Stop(site); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"prune", "off"} {
		unit := UnitName(site.Name, id) + ".timer"
		if !slices.Contains(mgr.calls, "stop:"+unit) {
			t.Errorf("pausing did not stop %s; calls were %v", unit, mgr.calls)
		}
		if !slices.Contains(mgr.calls, "disable:"+unit) {
			t.Errorf("pausing did not disable %s, so it comes back at the next boot; calls were %v", unit, mgr.calls)
		}
	}
	// The unit files stay. Resuming has to bring back what was scheduled, and
	// the entry is the only record of what that was.
	for _, id := range []string{"prune", "off"} {
		unit := UnitName(site.Name, id)
		if slices.Contains(mgr.calls, "remove-service:"+unit) || slices.Contains(mgr.calls, "remove-timer:"+unit) {
			t.Errorf("pausing deleted %s's units rather than stopping them", unit)
		}
	}
}

// Resuming schedules exactly what the site had, which means an entry the
// operator had switched off stays off. Pausing is not a way to turn a disabled
// command back on.
func TestResume_RestoresTheScheduleWithoutRevivingADisabledEntry(t *testing.T) {
	mgr := newRecordingMgr()
	swapMgr(t, mgr)
	site := testSite()
	site.Cron = []config.CronEntry{
		{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"},
		{ID: "off", Name: "Off", Command: "php artisan off", Calendar: "daily", Disabled: true},
	}

	if err := Resume(site); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(mgr.calls, "start:"+UnitName(site.Name, "prune")+".timer") {
		t.Errorf("resuming did not start the scheduled entry; calls were %v", mgr.calls)
	}
	if slices.Contains(mgr.calls, "start:"+UnitName(site.Name, "off")+".timer") {
		t.Error("resuming started an entry the operator had switched off")
	}
}

// RemoveAll is what unlink calls, and it walks the machine rather than the
// registry on purpose.
//
// Sync finds a unit whose entry is gone by comparing the two, and it only ever
// runs for a site that is still registered. A site being removed is the one
// moment that comparison stops being available: after this, nothing looks at
// this prefix again. A timer left here goes on firing a command at a directory
// that is not served any more, with no page in the panel from which to notice
// it or stop it.
func TestRemoveAll_TakesUnitsTheRegistryNoLongerKnowsAbout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	for _, name := range []string{"acme", "acme-two"} {
		if err := config.AddSite(config.Site{Name: name, Domains: []string{name + ".example"}, Path: t.TempDir()}); err != nil {
			t.Fatal(err)
		}
	}

	mgr := newRecordingMgr()
	mgr.existing = []string{
		"servlo-cron-acme-prune",
		// The entry this one belonged to never reached the registry: its save
		// wrote the unit, failed at the registry, and failed again rolling back.
		"servlo-cron-acme-orphan",
		"servlo-cron-acme-two-report",
		"servlo-queue-acme",
	}
	swapMgr(t, mgr)

	site := testSite()
	site.Cron = []config.CronEntry{{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}}

	if err := RemoveAll(site); err != nil {
		t.Fatal(err)
	}

	for _, gone := range []string{"servlo-cron-acme-prune", "servlo-cron-acme-orphan"} {
		if !slices.Contains(mgr.calls, "remove-service:"+gone) {
			t.Errorf("%s outlived the site; calls were %v", gone, mgr.calls)
		}
		if !slices.Contains(mgr.calls, "remove-timer:"+gone) {
			t.Errorf("the timer for %s outlived the site; calls were %v", gone, mgr.calls)
		}
	}
	for _, mustKeep := range []string{"servlo-cron-acme-two-report", "servlo-queue-acme"} {
		if slices.Contains(mgr.calls, "remove-service:"+mustKeep) {
			t.Errorf("removing one site took %s with it", mustKeep)
		}
	}
}
