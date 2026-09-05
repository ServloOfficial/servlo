package sitecron

import (
	"strings"
	"testing"

	"github.com/ServloOfficial/servlo/internal/config"
)

func testSite() config.Site {
	return config.Site{
		Name:       "acme",
		Domains:    []string{"acme-supply.com"},
		Path:       "/home/dev/code/acme",
		PHPVersion: "8.4",
	}
}

// The unit pair is what an operator inspects with systemctl, so its name has to
// follow the same rule every other servlo unit follows.
func TestUnitName(t *testing.T) {
	if got, want := UnitName("acme", "prune"), "servlo-cron-acme-prune"; got != want {
		t.Errorf("UnitName = %q, want %q", got, want)
	}
}

// The command runs where the site's workers and its deploy script run: inside
// the site's own container, from the site's directory.
func TestServiceUnit_RunsInTheSitesContainer(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "prune", Name: "Prune batches", Command: "php artisan queue:prune-batches", Calendar: "daily", CaptureOutput: true}

	unit, err := ServiceUnit(site, entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Type=oneshot",
		"servlo-php84-fpm",
		"'/home/dev/code/acme'",
		"--env=SERVLO_SITE=acme",
		"php artisan queue:prune-batches",
		"Description=Servlo cron Prune batches (acme)",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("the service unit does not contain %q:\n%s", want, unit)
		}
	}
	// A oneshot that systemd restarts is a cron entry that runs continuously.
	if strings.Contains(unit, "Restart=always") {
		t.Error("the service unit restarts, which would run the command in a loop")
	}
}

// Output capture is a per-entry decision, and the entry that turns it off has
// to actually stop writing to the journal rather than merely stop being read.
func TestServiceUnit_DiscardsOutputWhenCaptureIsOff(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "noisy", Name: "Noisy", Command: "php artisan ping", Calendar: "minutely"}

	unit, err := ServiceUnit(site, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, "StandardOutput=null") || !strings.Contains(unit, "StandardError=null") {
		t.Errorf("an entry with capture off still writes to the journal:\n%s", unit)
	}

	entry.CaptureOutput = true
	unit, err = ServiceUnit(site, entry)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unit, "=null") {
		t.Errorf("an entry with capture on discards its output:\n%s", unit)
	}
}

// A command is a shell line, and the shell that reads it is the one inside the
// container. systemd is not a shell: what it hands over has to survive its own
// quoting rules first.
func TestServiceUnit_PassesTheCommandThroughAShell(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{
		ID: "report", Name: "Report", Calendar: "daily",
		Command: `wp cron event run --due-now > /dev/null 2>&1 && echo "100% done"`,
	}

	unit, err := ServiceUnit(site, entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unit, `/bin/sh -c "`) {
		t.Errorf("the command is not handed to a shell:\n%s", unit)
	}
	// A bare % is a systemd specifier and a bare " ends the argument.
	if !strings.Contains(unit, `100%% done`) {
		t.Errorf("a percent sign was left for systemd to read as a specifier:\n%s", unit)
	}
	if !strings.Contains(unit, `\"100%% done\"`) {
		t.Errorf("a quote was left to end the argument early:\n%s", unit)
	}
	if strings.Contains(unit, "\n\n[Service]\nExecStart") {
		t.Error("the unit lost its ordering directives")
	}
}

// A command that would inject a directive never reaches a file. The check runs
// at the point of generation so no caller can be the one that forgot.
func TestServiceUnit_RefusesAnInjectedDirective(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "evil", Name: "Evil", Calendar: "daily", Command: "php x\nExecStartPost=/bin/rm -rf /"}
	if _, err := ServiceUnit(site, entry); err == nil {
		t.Fatal("a command carrying a newline produced a unit")
	}
}

// A host-proxy site has no container, so there is nowhere for a scheduled
// command to run and the panel has to say so rather than write a unit that
// fails every minute.
func TestServiceUnit_RefusesASiteWithNoContainer(t *testing.T) {
	site := testSite()
	site.HostPort = 3000
	entry := config.CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "daily"}
	if _, err := ServiceUnit(site, entry); err == nil {
		t.Fatal("a site with no container produced a unit")
	}
}

func TestTimerUnit(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Calendar: "Mon..Fri *-*-* 02:15:00"}

	unit, err := TimerUnit(site, entry)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"OnCalendar=Mon..Fri *-*-* 02:15:00",
		"Unit=servlo-cron-acme-prune.service",
		"WantedBy=timers.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("the timer unit does not contain %q:\n%s", want, unit)
		}
	}
}

// A calendar expression carrying a newline would add directives to the timer.
func TestTimerUnit_RefusesAnInjectedCalendar(t *testing.T) {
	site := testSite()
	entry := config.CronEntry{ID: "evil", Name: "Evil", Command: "php x", Calendar: "daily\nOnBootSec=1s"}
	if _, err := TimerUnit(site, entry); err == nil {
		t.Fatal("a calendar carrying a newline produced a unit")
	}
}
