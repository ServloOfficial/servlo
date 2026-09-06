package sitecron

import (
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/systemd"
)

// outputLines is how much of a run's output the panel is shown. Enough for a
// stack trace, short of turning the page into a log viewer: the whole thing is
// in the journal, and the panel says where.
const outputLines = 100

// Run is what happened the last time an entry ran.
type Run struct {
	// At is when the run finished, or when it started while it is still going.
	At time.Time `json:"at"`
	// OK is false for a run that failed and for one still in flight.
	OK       bool   `json:"ok"`
	Running  bool   `json:"running"`
	ExitCode int    `json:"exit_code"`
	Result   string `json:"result,omitempty"`
	// Output is what the command printed, oldest line first, empty when the
	// entry does not capture it.
	Output []string `json:"output,omitempty"`
}

// Status is one entry and what the machine says about it.
type Status struct {
	config.CronEntry
	// Unit is the systemd unit pair's name, shown so an operator can go and
	// look at the same thing the panel is describing.
	Unit string `json:"unit"`
	// NextRun is when the timer fires next; zero when it is not scheduled.
	NextRun time.Time `json:"next_run,omitempty"`
	// LastRun is nil when systemd has no record of this entry ever running,
	// which is a different thing from a run that failed.
	LastRun *Run `json:"last_run,omitempty"`
}

// Indirected so the handler and the composition below can be tested without a
// systemd to ask. Nothing else replaces them.
var (
	lastRunFn      = systemd.LastRun
	nextElapseFn   = systemd.TimerNextElapse
	journalSinceFn = systemd.JournalSince
)

// List is the site's entries with their run state attached, in the order the
// site stores them.
func List(site config.Site) []Status {
	out := make([]Status, 0, len(site.Cron))
	for _, e := range site.Cron {
		out = append(out, StatusOf(site, e))
	}
	return out
}

// StatusOf reads one entry's state back from systemd.
func StatusOf(site config.Site, e config.CronEntry) Status {
	unit := UnitName(site.Name, e.ID)
	s := Status{CronEntry: e, Unit: unit}
	if !e.Disabled {
		s.NextRun = nextElapseFn(unit)
	}

	run, ok := lastRunFn(unit)
	if !ok || !run.Known {
		return s
	}
	at := run.FinishedAt
	if at.IsZero() {
		at = run.StartedAt
	}
	s.LastRun = &Run{
		At:       at,
		OK:       run.OK(),
		Running:  run.Running,
		ExitCode: run.ExitCode,
		Result:   run.Result,
	}
	if e.CaptureOutput {
		s.LastRun.Output = journalSinceFn(unit, run.StartedAt, outputLines)
	}
	return s
}
