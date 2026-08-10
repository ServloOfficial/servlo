package logrotate

import (
	"strings"
	"testing"
)

// A unit that resolves servlo from PATH stops working the first time PATH
// changes, and the way that failure shows up is a disk that filled months
// later.
func TestServiceUnit_RunsServloByAbsolutePath(t *testing.T) {
	body, err := ServiceUnit("/home/deploy/.local/bin/servlo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "ExecStart=/home/deploy/.local/bin/servlo logs rotate") {
		t.Errorf("the unit does not run servlo by path:\n%s", body)
	}
}

// Every line of a value is a line of a unit file, so one carrying a newline
// writes whatever comes after it into the unit as another directive.
func TestServiceUnit_RefusesAPathThatWouldWriteExtraDirectives(t *testing.T) {
	_, err := ServiceUnit("/bin/servlo\nExecStartPost=/bin/sh -c 'curl evil'")
	if err == nil {
		t.Fatal("a path carrying a newline was written into a unit file")
	}
	if _, err := ServiceUnit(""); err == nil {
		t.Error("a unit was written with nothing to run")
	}
}

// Persistent, because a droplet that was off overnight should rotate when it
// comes back rather than skip the day.
func TestTimerUnit_SurvivesADropletThatWasOff(t *testing.T) {
	body := TimerUnit()
	if !strings.Contains(body, "Persistent=true") {
		t.Errorf("the timer skips a missed run:\n%s", body)
	}
	if !strings.Contains(body, "OnCalendar="+Calendar) {
		t.Errorf("the timer does not carry the schedule:\n%s", body)
	}
	if !strings.Contains(body, "Unit="+UnitName+".service") {
		t.Errorf("the timer does not name what it runs:\n%s", body)
	}
}

// journald is a system file and changing it needs root, so what servlo can do
// is hand over the exact command rather than run it (CLAUDE.md §3.2).
func TestJournalCommands_AreForAPersonToRun(t *testing.T) {
	cmds := JournalCommands("1G")
	if len(cmds) == 0 {
		t.Fatal("no commands")
	}
	var bounded, restarted bool
	for _, c := range cmds {
		if !strings.HasPrefix(c, "sudo ") {
			t.Errorf("a command that needs root is printed without sudo: %q", c)
		}
		if strings.Contains(c, "SystemMaxUse=1G") {
			bounded = true
		}
		if strings.Contains(c, "systemd-journald") {
			restarted = true
		}
	}
	if !bounded {
		t.Error("the commands do not actually bound the journal")
	}
	if !restarted {
		t.Error("the commands write the setting and never make journald read it")
	}
}
