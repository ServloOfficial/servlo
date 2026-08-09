package sitecron

import (
	"testing"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/systemd"
)

// stubSystemd replaces the three reads that need a running systemd, so the
// composition below can be checked without one.
func stubSystemd(t *testing.T, runs map[string]systemd.UnitRun, next map[string]time.Time, output []string) {
	t.Helper()
	prevRun, prevNext, prevJournal := lastRunFn, nextElapseFn, journalSinceFn
	lastRunFn = func(unit string) (systemd.UnitRun, bool) {
		r, ok := runs[unit]
		return r, ok
	}
	nextElapseFn = func(unit string) time.Time { return next[unit] }
	journalSinceFn = func(string, time.Time, int) []string { return output }
	t.Cleanup(func() { lastRunFn, nextElapseFn, journalSinceFn = prevRun, prevNext, prevJournal })
}

// The three states the panel has to tell apart: never run, ran and failed, ran
// and worked. A row that cannot say "this has never run" reads as a job that
// silently does nothing.
func TestList_ReportsNeverRunSeparatelyFromFailed(t *testing.T) {
	site := testSite()
	site.Cron = []config.CronEntry{
		{ID: "fresh", Name: "Fresh", Command: "php x", Calendar: "daily"},
		{ID: "broken", Name: "Broken", Command: "php y", Calendar: "hourly", CaptureOutput: true},
		{ID: "fine", Name: "Fine", Command: "php z", Calendar: "daily", CaptureOutput: true},
	}
	finished := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	stubSystemd(t,
		map[string]systemd.UnitRun{
			"servlo-cron-acme-fresh":  {},
			"servlo-cron-acme-broken": {Known: true, Result: "exit-code", ExitCode: 1, StartedAt: finished, FinishedAt: finished},
			"servlo-cron-acme-fine":   {Known: true, Result: "success", StartedAt: finished, FinishedAt: finished},
		},
		map[string]time.Time{"servlo-cron-acme-fine": finished.Add(time.Hour)},
		[]string{"one", "two"},
	)

	got := List(site)
	if len(got) != 3 {
		t.Fatalf("List returned %d entries, want 3", len(got))
	}
	if got[0].LastRun != nil {
		t.Errorf("an entry that never ran reported a last run: %+v", got[0].LastRun)
	}
	if got[1].LastRun == nil || got[1].LastRun.OK {
		t.Errorf("a failed run did not report as failed: %+v", got[1].LastRun)
	}
	if got[1].LastRun.ExitCode != 1 || got[1].LastRun.Result != "exit-code" {
		t.Errorf("the failure lost systemd's own account of it: %+v", got[1].LastRun)
	}
	if got[2].LastRun == nil || !got[2].LastRun.OK {
		t.Errorf("a clean run did not report as OK: %+v", got[2].LastRun)
	}
	if got[2].NextRun.IsZero() {
		t.Error("a scheduled entry reported no next run")
	}
	if len(got[2].LastRun.Output) != 2 {
		t.Errorf("captured output did not reach the status: %+v", got[2].LastRun)
	}
	if got[0].Unit != "servlo-cron-acme-fresh" {
		t.Errorf("Unit = %q, which is not the unit an operator would look for", got[0].Unit)
	}
}

// An entry that does not capture output has none to show, and reading the
// journal for it would show the previous run's, or another job's.
func TestStatusOf_NoOutputWhenCaptureIsOff(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "quiet", Name: "Quiet", Command: "php x", Calendar: "daily"}
	at := time.Date(2026, 8, 9, 3, 0, 0, 0, time.UTC)
	stubSystemd(t,
		map[string]systemd.UnitRun{"servlo-cron-acme-quiet": {Known: true, Result: "success", StartedAt: at, FinishedAt: at}},
		nil, []string{"should not be read"},
	)

	got := StatusOf(site, entry)
	if got.LastRun == nil {
		t.Fatal("a run that happened was not reported")
	}
	if len(got.LastRun.Output) != 0 {
		t.Errorf("output was shown for an entry that does not capture it: %v", got.LastRun.Output)
	}
}

// A disabled entry is not scheduled, so a next run would be a time that never
// arrives.
func TestStatusOf_ADisabledEntryHasNoNextRun(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "off", Name: "Off", Command: "php x", Calendar: "daily", Disabled: true}
	stubSystemd(t, nil, map[string]time.Time{"servlo-cron-acme-off": time.Now()}, nil)

	if got := StatusOf(site, entry); !got.NextRun.IsZero() {
		t.Errorf("a disabled entry reported a next run at %v", got.NextRun)
	}
}
