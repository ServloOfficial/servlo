package systemd

import "testing"

// A run that ended cleanly is the only one that reads as OK. Everything else,
// including a unit systemd has no record of, is not a success anybody observed.
func TestUnitRunOK(t *testing.T) {
	cases := []struct {
		what string
		run  UnitRun
		want bool
	}{
		{"a clean run", UnitRun{Known: true, Result: "success"}, true},
		{"a non-zero exit", UnitRun{Known: true, Result: "success", ExitCode: 1}, false},
		{"a failure", UnitRun{Known: true, Result: "exit-code", ExitCode: 2}, false},
		{"a unit that never ran", UnitRun{}, false},
		{"a timeout", UnitRun{Known: true, Result: "timeout"}, false},
	}
	for _, c := range cases {
		if got := c.run.OK(); got != c.want {
			t.Errorf("%s: OK() = %v, want %v", c.what, got, c.want)
		}
	}
}

// Without systemd there is no run history, and the honest answer is to say so
// rather than report a unit that has never run as one that succeeded.
func TestLastRun_WithoutABus(t *testing.T) {
	run, _ := LastRun("servlo-cron-nonexistent-nothing")
	if run.OK() {
		t.Error("a unit that does not exist reported a successful run")
	}
	if !TimerNextElapse("servlo-cron-nonexistent-nothing").IsZero() {
		t.Error("a timer that does not exist reported a next run")
	}
}

// A cron unit is named servlo-cron-<site>-<id>, which for a site whose name is
// somebody else's entry id reads as a worker called "cron-<owner>". Stopping it
// as an orphan would take another site's schedule down.
func TestOrphanCandidate_IgnoresAnotherSitesCronUnit(t *testing.T) {
	// acme's nightly entry, on a machine that also has a site called "nightly".
	if worker, ok := orphanCandidate("servlo-cron-acme-nightly.service", "nightly", nil, nil, false); ok {
		t.Fatalf("another site's cron unit was reported as the orphaned worker %q", worker)
	}
	// A real orphaned worker of that site still is one.
	if worker, ok := orphanCandidate("servlo-queue-nightly.service", "nightly", nil, nil, false); !ok || worker != "queue" {
		t.Errorf("orphanCandidate(queue) = %q, %v; want the worker reported", worker, ok)
	}
}
