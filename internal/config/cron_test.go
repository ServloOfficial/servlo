package config

import (
	"strings"
	"testing"
)

func TestCronID_IsUsableInAUnitFileName(t *testing.T) {
	cases := map[string]string{
		"Prune batches":         "prune-batches",
		"  WordPress  cron  ":   "wordpress-cron",
		"php artisan queue:run": "php-artisan-queue-run",
		"…":                     "",
		"":                      "",
	}
	for in, want := range cases {
		if got := CronID(in); got != want {
			t.Errorf("CronID(%q) = %q, want %q", in, got, want)
		}
	}
	long := CronID(strings.Repeat("report ", 20))
	if len(long) > 40 || !cronID.MatchString(long) {
		t.Errorf("CronID of a long name = %q, which a unit file name cannot hold", long)
	}
}

// Every field of an entry is a line of a generated unit, and a newline in one
// of them is a directive nobody wrote.
func TestCronEntryValidate_RefusesWhatWouldInjectAUnitDirective(t *testing.T) {
	base := CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune", Schedule: "daily"}
	if err := base.Validate(); err != nil {
		t.Fatalf("a plain entry was refused: %v", err)
	}

	bad := []struct {
		what  string
		entry CronEntry
	}{
		{"a newline in the command", func() CronEntry { e := base; e.Command = "php x\nExecStart=/bin/sh"; return e }()},
		{"a newline in the name", func() CronEntry { e := base; e.Name = "Prune\nRestart=always"; return e }()},
		{"an empty command", func() CronEntry { e := base; e.Command = "  "; return e }()},
		{"an empty name", func() CronEntry { e := base; e.Name = ""; return e }()},
		{"an identifier with a slash", func() CronEntry { e := base; e.ID = "../../etc/x"; return e }()},
		{"an empty identifier", func() CronEntry { e := base; e.ID = ""; return e }()},
		{"a name past the ceiling", func() CronEntry { e := base; e.Name = strings.Repeat("n", CronNameCeiling+1); return e }()},
		{"a command past the ceiling", func() CronEntry {
			e := base
			e.Command = strings.Repeat("c", CronCommandCeiling+1)
			return e
		}()},
	}
	for _, c := range bad {
		if err := c.entry.Validate(); err == nil {
			t.Errorf("%s was accepted", c.what)
		}
	}
}

// systemd expands $NAME on a command line, and an unset one expands to nothing,
// so a command carrying one would run as something other than what it says.
func TestCronEntryValidate_RefusesADollarInTheCommand(t *testing.T) {
	e := CronEntry{ID: "prune", Name: "Prune", Command: "php artisan prune --path=$HOME", Schedule: "daily"}
	err := e.Validate()
	if err == nil {
		t.Fatal("a command containing $ was accepted")
	}
	if !strings.Contains(err.Error(), "script") {
		t.Errorf("the refusal %q does not say where to put the command instead", err)
	}
}

func TestFindCron(t *testing.T) {
	site := &Site{Cron: []CronEntry{{ID: "a"}, {ID: "b"}}}
	if e, ok := site.FindCron("b"); !ok || e.ID != "b" {
		t.Errorf("FindCron(b) = %v, %v", e, ok)
	}
	if _, ok := site.FindCron("c"); ok {
		t.Error("FindCron found an entry that is not there")
	}
}
