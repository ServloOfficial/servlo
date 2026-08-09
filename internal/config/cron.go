package config

import (
	"fmt"
	"regexp"
	"strings"
)

// CronEntry is one scheduled command on a site.
//
// It is stored with the site rather than in a file of its own, because it is a
// property of the site in the same way its PHP version is: a site that moves,
// pauses or is removed takes its schedule with it, and a registry that already
// knows every site is the one place that cannot fall out of step with them.
type CronEntry struct {
	// ID is the stable handle: it names the unit pair, so it must survive a
	// rename of the entry and must be usable in a filename.
	ID string `yaml:"id" json:"id"`
	// Name is what the panel shows. Free text, and changing it renames nothing.
	Name string `yaml:"name" json:"name"`
	// Command runs inside the site's PHP container, from the site's directory,
	// the same way its workers and its deploy script do.
	Command string `yaml:"command" json:"command"`
	// Schedule is what the operator typed, kept verbatim so the form shows it
	// back to them rather than showing the systemd expression it became.
	Schedule string `yaml:"schedule" json:"schedule"`
	// Calendar is the OnCalendar expression Schedule was translated into. Stored
	// so the unit can be rewritten without re-parsing, and so a change in the
	// parser cannot silently change when an existing entry runs.
	Calendar string `yaml:"calendar" json:"calendar"`
	// CaptureOutput keeps what the command printed in the journal, which is
	// where the panel reads the last run's output from. Off sends it to
	// /dev/null, for a job that is chatty every minute.
	CaptureOutput bool `yaml:"capture_output,omitempty" json:"capture_output"`
	// Disabled keeps the entry but stops the timer. An operator turning a job
	// off for an afternoon should not have to retype it afterwards.
	Disabled bool `yaml:"disabled,omitempty" json:"disabled"`
	// Managed marks an entry servlo installed on the framework's behalf, so the
	// panel offers it as the toggle it is rather than as a row to hand-edit.
	Managed bool `yaml:"managed,omitempty" json:"managed,omitempty"`
}

// cronID is what an entry's identifier may look like. It becomes part of a
// systemd unit file name, so it is limited to what a unit name accepts and to a
// length that leaves the site name room.
var cronID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// CronNameCeiling is how long an entry's name may be. It lands in a unit's
// Description=, which is read in a terminal.
const CronNameCeiling = 80

// CronCommandCeiling is how long a command may be. Past it the operator wants a
// script in the site, not a line in a form.
const CronCommandCeiling = 512

// CronID turns a name into a usable identifier: lowercase, separators
// collapsed, nothing a unit file name would object to. Empty when the name
// carried no usable characters, which is the caller's cue to supply its own.
func CronID(name string) string {
	var b strings.Builder
	lastDash := true // leading dashes are dropped by starting as if one was written
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if len(id) > 40 {
		id = strings.Trim(id[:40], "-")
	}
	// An identifier has to start with a letter or a digit, and stripping the
	// separators can leave one that starts with neither only if it is empty.
	if !cronID.MatchString(id) {
		return ""
	}
	return id
}

// Validate refuses an entry that would produce a unit servlo could not stand
// behind. The schedule is not checked here: translating it needs the calendar
// parser, and this package is what that parser is built on.
func (e CronEntry) Validate() error {
	if !cronID.MatchString(e.ID) {
		return fmt.Errorf("%q is not a usable identifier for a scheduled command: lower-case letters, digits and dashes", e.ID)
	}
	switch {
	case strings.TrimSpace(e.Name) == "":
		return fmt.Errorf("a scheduled command needs a name")
	case len(e.Name) > CronNameCeiling:
		return fmt.Errorf("the name is %d characters, which is past the %d a unit description holds", len(e.Name), CronNameCeiling)
	case strings.TrimSpace(e.Command) == "":
		return fmt.Errorf("a scheduled command needs a command to run")
	case len(e.Command) > CronCommandCeiling:
		return fmt.Errorf("the command is %d characters, which is past the %d a schedule entry holds: put it in a script in the site and call that", len(e.Command), CronCommandCeiling)
	case ContainsUnitInjectionChars(e.Name) || ContainsUnitInjectionChars(e.Command):
		return fmt.Errorf("a scheduled command must not contain a newline or a NUL: every line of it is a line of a unit file")
	case strings.Contains(e.Command, "$"):
		// systemd expands $NAME on a command line before the shell ever sees
		// it, and an unset one expands to nothing. A command that quietly loses
		// half of itself is worse than one that would not save, so it is
		// refused here with somewhere else to put it.
		return fmt.Errorf("a scheduled command cannot contain $: systemd reads it as one of its own variables. Put the work in a script in the site and schedule that")
	}
	return nil
}

// FindCron returns the site's entry with this id.
func (s *Site) FindCron(id string) (CronEntry, bool) {
	for _, e := range s.Cron {
		if e.ID == id {
			return e, true
		}
	}
	return CronEntry{}, false
}
