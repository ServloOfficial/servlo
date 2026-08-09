//go:build !linux

package systemd

import "time"

// LastRun has no answer without systemd: the run history lives in systemd, and
// inventing one would be a panel that reports success it never observed.
func LastRun(string) (UnitRun, bool) { return UnitRun{}, false }

// TimerNextElapse has no answer without systemd.
func TimerNextElapse(string) time.Time { return time.Time{} }

// JournalSince has no answer without a journal.
func JournalSince(string, time.Time, int) []string { return nil }
