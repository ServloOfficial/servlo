//go:build linux

package systemd

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// LastRun reads what systemd remembers about the named service's last
// invocation. The second return is false when systemd does not know the unit at
// all, which is a different answer from a unit it knows and has never run.
func LastRun(name string) (UnitRun, bool) {
	conn, err := userConn()
	if err != nil {
		return UnitRun{}, false
	}
	unit := withServiceSuffix(name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	props, err := conn.GetUnitTypePropertiesContext(ctx, unit, "Service")
	if err != nil {
		return UnitRun{}, false
	}
	state, _ := conn.GetUnitPropertiesContext(ctx, unit)

	run := UnitRun{
		StartedAt:  usecToTime(props["ExecMainStartTimestamp"]),
		FinishedAt: usecToTime(props["ExecMainExitTimestamp"]),
	}
	if code, ok := props["ExecMainStatus"].(int32); ok {
		run.ExitCode = int(code)
	}
	run.Result, _ = props["Result"].(string)
	if active, ok := state["ActiveState"].(string); ok {
		run.Running = active == "activating" || active == "active"
	}
	// A unit that has never run reports a zero start and an empty result. Only
	// then is there nothing to say; a unit that ran and failed reports both.
	run.Known = !run.StartedAt.IsZero() || (run.Result != "" && run.Result != "success")
	return run, true
}

// TimerNextElapse is when the named timer fires next, or the zero time when
// systemd has no answer (the timer is not loaded, or it is disabled).
func TimerNextElapse(name string) time.Time {
	conn, err := userConn()
	if err != nil {
		return time.Time{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	props, err := conn.GetUnitTypePropertiesContext(ctx, withTimerSuffix(name), "Timer")
	if err != nil {
		return time.Time{}
	}
	return usecToTime(props["NextElapseUSecRealtime"])
}

// JournalSince returns up to max message lines the unit logged at or after
// since, oldest first. An empty since reads the whole retained journal for the
// unit, which is what a caller wants when systemd has forgotten when the last
// run started.
func JournalSince(name string, since time.Time, max int) []string {
	unit := withServiceSuffix(name)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	args := []string{"--user", "-u", unit, "--no-pager", "-o", "cat", "-n", fmt.Sprintf("%d", max)}
	if !since.IsZero() {
		args = append(args, "--since", fmt.Sprintf("@%d", since.Unix()))
	}
	out, err := exec.CommandContext(ctx, "journalctl", args...).Output()
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if line == "" || line == "-- No entries --" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// usecToTime converts one of systemd's microsecond timestamps. Zero means the
// event has not happened, and systemd also uses the maximum value for "never".
func usecToTime(v any) time.Time {
	usec, ok := v.(uint64)
	if !ok || usec == 0 || usec == ^uint64(0) {
		return time.Time{}
	}
	return time.UnixMicro(int64(usec)).UTC()
}

func withTimerSuffix(name string) string {
	if strings.HasSuffix(name, ".timer") {
		return name
	}
	return strings.TrimSuffix(name, ".service") + ".timer"
}
