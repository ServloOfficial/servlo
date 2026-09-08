package backup

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/services"
)

type stateUnitRecorder struct {
	services.ServiceManager
	units   map[string]string
	timers  map[string]string
	enabled []string
	started []string
	removed []string
	reloads int
}

func newStateUnitRecorder() *stateUnitRecorder {
	return &stateUnitRecorder{
		ServiceManager: services.Mgr,
		units:          map[string]string{},
		timers:         map[string]string{},
	}
}

func (m *stateUnitRecorder) WriteServiceUnitIfChanged(name, body string) (bool, error) {
	m.units[name] = body
	return true, nil
}

func (m *stateUnitRecorder) WriteTimerUnitIfChanged(name, body string) (bool, error) {
	m.timers[name] = body
	return true, nil
}

func (m *stateUnitRecorder) DaemonReload() error { m.reloads++; return nil }
func (m *stateUnitRecorder) Enable(unit string) error {
	m.enabled = append(m.enabled, unit)
	return nil
}
func (m *stateUnitRecorder) Start(unit string) error { m.started = append(m.started, unit); return nil }
func (m *stateUnitRecorder) Stop(string) error       { return nil }
func (m *stateUnitRecorder) Disable(string) error    { return nil }
func (m *stateUnitRecorder) RemoveTimerUnit(n string) error {
	m.removed = append(m.removed, n+".timer")
	return nil
}
func (m *stateUnitRecorder) RemoveServiceUnit(n string) error {
	m.removed = append(m.removed, n+".service")
	return nil
}

func swapManager(t *testing.T, m services.ServiceManager) {
	t.Helper()
	prev := services.Mgr
	services.Mgr = m
	t.Cleanup(func() { services.Mgr = prev })
}

// Nothing put the server's own state on a timer.
//
// Every site could be on a nightly schedule with its archives going offsite,
// and the archive that makes any of them restorable was written only when
// somebody remembered to type the command. A rebuild then restores files and
// databases onto a server holding whatever the registry looked like the last
// time a human thought about it.
func TestApplyStateSchedule_ArmsTheServersOwnBackup(t *testing.T) {
	rec := newStateUnitRecorder()
	swapManager(t, rec)

	if err := ApplyStateSchedule(true, "/home/servlo/.local/bin/servlo"); err != nil {
		t.Fatal(err)
	}

	service, ok := rec.units[StateUnitName]
	if !ok {
		t.Fatalf("no service unit was written; units = %v", rec.units)
	}
	if !strings.Contains(service, "/home/servlo/.local/bin/servlo backup state") {
		t.Errorf("the unit does not run the state backup by absolute path:\n%s", service)
	}

	timer, ok := rec.timers[StateUnitName]
	if !ok {
		t.Fatalf("no timer unit was written; timers = %v", rec.timers)
	}
	// Persistent, because the night the droplet was rebooted is the night
	// worth having.
	if !strings.Contains(timer, "Persistent=true") {
		t.Errorf("the timer skips a missed night:\n%s", timer)
	}
	if !strings.Contains(timer, "OnCalendar="+StateCalendar) {
		t.Errorf("the timer does not carry its schedule:\n%s", timer)
	}

	if len(rec.enabled) != 1 || rec.enabled[0] != StateUnitName+".timer" {
		t.Errorf("the timer was written but not enabled: %v", rec.enabled)
	}
	if len(rec.started) != 1 {
		t.Errorf("the timer was enabled but not started: %v", rec.started)
	}
	if rec.reloads == 0 {
		t.Error("systemd was not reloaded, so the unit it has is the old one")
	}
}

// Switched off, the units go rather than being left disabled for somebody to
// find later and wonder about.
func TestApplyStateSchedule_TakesTheUnitsAway(t *testing.T) {
	rec := newStateUnitRecorder()
	swapManager(t, rec)

	if err := ApplyStateSchedule(false, "/home/servlo/.local/bin/servlo"); err != nil {
		t.Fatal(err)
	}
	if len(rec.removed) != 2 {
		t.Errorf("removed = %v, want both the timer and the service", rec.removed)
	}
	if len(rec.units) != 0 || len(rec.timers) != 0 {
		t.Error("units were written while turning the schedule off")
	}
}

// Every line of a unit file is a line of a unit file. A path carrying a newline
// writes whatever follows it in as another directive.
func TestStateServiceUnit_RefusesAnInjectedPath(t *testing.T) {
	for _, bad := range []string{"", "/bin/servlo\nExecStartPost=/bin/rm -rf /", "/bin/servlo\x00"} {
		if _, err := StateServiceUnit(bad); err == nil {
			t.Errorf("StateServiceUnit(%q) was accepted", bad)
		}
	}
}
