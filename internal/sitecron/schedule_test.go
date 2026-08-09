package sitecron

import (
	"strings"
	"testing"
)

// The forms an operator actually types. Cron is what they know from crontab and
// from every other panel; the systemd spellings are what the unit file wants.
// Both have to arrive at an OnCalendar expression systemd accepts.
func TestNormalizeSchedule_AcceptsCronAndCalendar(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"* * * * *", "*-*-* *:*:00"},
		{"*/5 * * * *", "*-*-* *:0/5:00"},
		{"0 3 * * *", "*-*-* 03:00:00"},
		{"15 2 * * 1-5", "Mon..Fri *-*-* 02:15:00"},
		{"0 0 1 * *", "*-*-01 00:00:00"},
		{"30 4 * * 0", "Sun *-*-* 04:30:00"},
		{"0,30 * * * *", "*-*-* *:00,30:00"},
		{"0 */2 * * *", "*-*-* 0/2:00:00"},
		{"0 9 * jan mon", "Mon *-01-* 09:00:00"},
		{"@daily", "daily"},
		{"@hourly", "hourly"},
		{"@weekly", "weekly"},
		{"@monthly", "monthly"},
		{"@yearly", "yearly"},
		// Already a systemd expression: kept as typed, only trimmed.
		{"daily", "daily"},
		{"minutely", "minutely"},
		{"  Mon *-*-* 02:00:00  ", "Mon *-*-* 02:00:00"},
		{"*-*-* 04:00:00", "*-*-* 04:00:00"},
		{"Mon..Fri *-*-* 09:30:00", "Mon..Fri *-*-* 09:30:00"},
		{"*:0/10", "*:0/10"},
	}
	for _, c := range cases {
		got, err := NormalizeSchedule(c.in)
		if err != nil {
			t.Errorf("NormalizeSchedule(%q) errored: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("NormalizeSchedule(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A schedule nobody can parse must say what a good one looks like. A parser
// error names the state machine that gave up, which tells an operator nothing
// about what to type instead.
func TestNormalizeSchedule_RefusesWithAnExample(t *testing.T) {
	for _, in := range []string{"", "every 5 minutes", "* * *", "61 * * * *", "0 3 * * funday", "tuesday at nine"} {
		got, err := NormalizeSchedule(in)
		if err == nil {
			t.Errorf("NormalizeSchedule(%q) = %q, want an error", in, got)
			continue
		}
		if !containsAll(err.Error(), "*/5 * * * *", "daily") {
			t.Errorf("NormalizeSchedule(%q) error %q names no example to copy", in, err)
		}
	}
}

// Cron reads a day-of-month and a day-of-week together as "either", and a
// systemd calendar reads its two as "both". Silently translating one into the
// other would run the job on days the operator did not ask for, so it is
// refused instead.
func TestNormalizeSchedule_RefusesDayOfMonthAndWeekTogether(t *testing.T) {
	if _, err := NormalizeSchedule("0 0 1 * mon"); err == nil {
		t.Fatal("a cron line with both a day-of-month and a day-of-week was accepted")
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
