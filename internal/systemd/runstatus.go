package systemd

import "time"

// UnitRun is what systemd remembers about the last time it ran a unit.
//
// It is read back from systemd rather than kept by whoever started the unit,
// which is the point: a panel that restarts must still be able to say what a
// scheduled command did at four in the morning, and the only process that was
// there is systemd.
type UnitRun struct {
	// Known is false when systemd has no record of the unit ever running,
	// which for a freshly written timer is the ordinary case and is a
	// different thing from a run that failed.
	Known bool
	// StartedAt and FinishedAt are the last invocation's boundaries. FinishedAt
	// is zero while the unit is still running.
	StartedAt  time.Time
	FinishedAt time.Time
	// ExitCode is the main process's exit status.
	ExitCode int
	// Result is systemd's own word for how it ended: "success", "exit-code",
	// "timeout", "signal", "oom-kill", "core-dump". Shown as-is, because it is
	// the word the operator will find again in journalctl.
	Result string
	// Running is true while the invocation is still going.
	Running bool
}

// OK reports whether the last run finished the way it was supposed to.
func (r UnitRun) OK() bool { return r.Known && r.Result == "success" && r.ExitCode == 0 }
