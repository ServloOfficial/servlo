// Package sitecron is a site's own scheduled commands: what runs, how often,
// and what happened the last time it ran.
//
// One systemd user timer and one oneshot service per entry, named the way every
// other servlo unit is named, so the thing an operator inspects with systemctl
// is the same thing the panel is showing them. Nothing is kept in the panel's
// memory: the schedule lives in the site's registry entry and the run history
// lives in the journal, which is why both survive a restart of servlo itself.
package sitecron

import (
	"fmt"
	"strconv"
	"strings"
)

// scheduleHelp is appended to every schedule the parser gives up on. An
// operator who typed something wrong needs a line they can copy, not the
// position at which a state machine stopped agreeing with them.
const scheduleHelp = `use cron ("*/5 * * * *", "0 3 * * 1-5") or a systemd calendar ("daily", "Mon *-*-* 02:00:00")`

// cronShorthands are the @-prefixed names crontab accepts. systemd spells the
// same five ideas as bare words, so the translation is a rename.
var cronShorthands = map[string]string{
	"@yearly":   "yearly",
	"@annually": "yearly",
	"@monthly":  "monthly",
	"@weekly":   "weekly",
	"@daily":    "daily",
	"@midnight": "daily",
	"@hourly":   "hourly",
}

// calendarKeywords are the systemd calendar names that stand alone.
var calendarKeywords = map[string]bool{
	"minutely": true, "hourly": true, "daily": true, "weekly": true,
	"monthly": true, "quarterly": true, "semiannually": true,
	"yearly": true, "annually": true,
}

// weekdayNames are systemd's day names, indexed the way cron numbers them
// (0 and 7 both being Sunday).
var weekdayNames = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// weekdayNumbers maps the names crontab accepts onto cron's own numbering.
var weekdayNumbers = map[string]int{
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// monthNumbers maps the month names crontab accepts onto their numbers, which
// is what a systemd calendar wants in the month position.
var monthNumbers = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// NormalizeSchedule turns what an operator typed into an OnCalendar expression.
//
// Three input forms are accepted, because those are the three an operator
// arrives with: a crontab line, a crontab @shorthand, and a systemd calendar
// expression they copied from another unit. Anything else is refused with an
// example rather than translated into a guess, since a schedule that runs at
// the wrong time is worse than one that would not save.
func NormalizeSchedule(in string) (string, error) {
	s := strings.Join(strings.Fields(in), " ")
	if s == "" {
		return "", fmt.Errorf("this entry needs a schedule: %s", scheduleHelp)
	}
	if expanded, ok := cronShorthands[strings.ToLower(s)]; ok {
		return expanded, nil
	}
	if calendarKeywords[strings.ToLower(s)] {
		return strings.ToLower(s), nil
	}
	if fields := strings.Split(s, " "); len(fields) == 5 && !strings.Contains(s, ":") {
		return cronToCalendar(fields)
	}
	if err := validateCalendar(s); err != nil {
		return "", err
	}
	return s, nil
}

// cronToCalendar translates the five crontab fields into a calendar expression.
func cronToCalendar(f []string) (string, error) {
	minute, err := cronField(f[0], 0, 59, nil)
	if err != nil {
		return "", scheduleError("minute", f[0])
	}
	hour, err := cronField(f[1], 0, 23, nil)
	if err != nil {
		return "", scheduleError("hour", f[1])
	}
	dom, err := cronField(f[2], 1, 31, nil)
	if err != nil {
		return "", scheduleError("day of month", f[2])
	}
	month, err := cronField(f[3], 1, 12, monthNumbers)
	if err != nil {
		return "", scheduleError("month", f[3])
	}
	dow, err := cronField(f[4], 0, 7, weekdayNumbers)
	if err != nil {
		return "", scheduleError("day of week", f[4])
	}

	// cron runs a line with both a day-of-month and a day-of-week on either of
	// them; a systemd calendar needs both to match at once. There is no
	// expression that means the first, so the two together are refused rather
	// than quietly narrowed to the days they have in common.
	if dow != "*" && dom != "*" {
		return "", fmt.Errorf("a day of the month and a day of the week together mean %q in cron, which a systemd timer cannot express: pick one", "either")
	}

	calendar := fmt.Sprintf("*-%s-%s %s:%s:00", month, dom, hour, minute)
	if dow != "*" {
		calendar = namedWeekdays(dow) + " " + calendar
	}
	return calendar, nil
}

// cronField translates one crontab field into its calendar equivalent: a step
// counts from the field's own floor, a range is spelled with two dots, and a
// list stays a list.
func cronField(spec string, min, max int, names map[string]int) (string, error) {
	var parts []string
	for _, item := range strings.Split(spec, ",") {
		converted, err := cronFieldItem(strings.TrimSpace(item), min, max, names)
		if err != nil {
			return "", err
		}
		parts = append(parts, converted)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	// A list containing "*" is the same as "*", and systemd rejects the mixture.
	for _, p := range parts {
		if p == "*" {
			return "*", nil
		}
	}
	return strings.Join(parts, ","), nil
}

func cronFieldItem(item string, min, max int, names map[string]int) (string, error) {
	if item == "" {
		return "", fmt.Errorf("empty")
	}
	body, step := item, ""
	if slash := strings.Index(item, "/"); slash >= 0 {
		body, step = item[:slash], item[slash+1:]
		n, err := strconv.Atoi(step)
		if err != nil || n < 1 {
			return "", fmt.Errorf("bad step %q", step)
		}
	}

	var out string
	switch {
	case body == "*":
		// A step counts from the field's floor: cron's "*/2" in the hour field
		// is 0,2,4…, and in the day-of-month field it is the 1st, 3rd, 5th.
		if step == "" {
			return "*", nil
		}
		out = strconv.Itoa(min)
	case strings.Contains(body, "-"):
		bounds := strings.SplitN(body, "-", 2)
		lo, err := cronValue(bounds[0], min, max, names)
		if err != nil {
			return "", err
		}
		hi, err := cronValue(bounds[1], min, max, names)
		if err != nil {
			return "", err
		}
		if lo > hi {
			return "", fmt.Errorf("range runs backwards")
		}
		out = fmt.Sprintf("%02d..%02d", lo, hi)
	default:
		v, err := cronValue(body, min, max, names)
		if err != nil {
			return "", err
		}
		out = fmt.Sprintf("%02d", v)
	}
	if step != "" {
		// systemd's own normalisation drops the leading zero on a stepped
		// floor, so match it: "0/5" rather than "00/5".
		out = strings.TrimPrefix(out, "0")
		if out == "" {
			out = "0"
		}
		out += "/" + step
	}
	return out, nil
}

func cronValue(s string, min, max int, names map[string]int) (int, error) {
	s = strings.TrimSpace(s)
	if names != nil {
		if v, ok := names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("not a number")
	}
	if v < min || v > max {
		return 0, fmt.Errorf("outside %d to %d", min, max)
	}
	return v, nil
}

// namedWeekdays rewrites a translated day-of-week field in systemd's names.
// cron numbers Sunday both 0 and 7 and systemd names it once, so both land on
// "Sun".
func namedWeekdays(field string) string {
	var out []string
	for _, item := range strings.Split(field, ",") {
		if lo, hi, ok := strings.Cut(item, ".."); ok {
			out = append(out, dayName(lo)+".."+dayName(hi))
			continue
		}
		out = append(out, dayName(item))
	}
	return strings.Join(out, ",")
}

func dayName(s string) string {
	// A stepped day-of-week ("1/2") has no name, so it is left as systemd's
	// numeric form, which systemd accepts alongside the names.
	n, err := strconv.Atoi(strings.TrimLeft(s, "0"))
	if err != nil {
		if s == "00" || s == "0" {
			return weekdayNames[0]
		}
		return s
	}
	if n < 0 || n >= len(weekdayNames) {
		return s
	}
	return weekdayNames[n]
}

// calendarChars are the characters a systemd calendar's date and time
// components are built from.
const calendarChars = "0123456789*/,.~"

// validateCalendar refuses an expression systemd would reject, without
// reimplementing systemd's parser: a leading day-of-week list of names, then an
// optional date, then a time. That covers what an operator copies from a unit
// file, and anything stranger is better refused here than accepted into a timer
// that silently never fires.
func validateCalendar(s string) error {
	fields := strings.Split(s, " ")
	if len(fields) > 0 && isWeekdayList(fields[0]) {
		fields = fields[1:]
	}
	switch len(fields) {
	case 1:
		if !isTimeComponent(fields[0]) {
			return scheduleError("time", fields[0])
		}
	case 2:
		if !isDateComponent(fields[0]) {
			return scheduleError("date", fields[0])
		}
		if !isTimeComponent(fields[1]) {
			return scheduleError("time", fields[1])
		}
	default:
		return fmt.Errorf("%q is not a schedule servlo can read: %s", s, scheduleHelp)
	}
	return nil
}

func isWeekdayList(s string) bool {
	if s == "" {
		return false
	}
	for _, item := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' }) {
		for _, day := range strings.Split(item, "..") {
			if _, ok := weekdayNumbers[strings.ToLower(strings.TrimSuffix(day, "day"))]; !ok {
				if !knownWeekday(day) {
					return false
				}
			}
		}
	}
	return true
}

func knownWeekday(s string) bool {
	lower := strings.ToLower(s)
	for full, short := range map[string]string{
		"sunday": "sun", "monday": "mon", "tuesday": "tue", "wednesday": "wed",
		"thursday": "thu", "friday": "fri", "saturday": "sat",
	} {
		if lower == full || lower == short {
			return true
		}
	}
	_, ok := weekdayNumbers[lower]
	return ok
}

func isDateComponent(s string) bool {
	parts := strings.Split(s, "-")
	if len(parts) != 3 && len(parts) != 2 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsFunc(p, func(r rune) bool {
			return !strings.ContainsRune(calendarChars, r)
		}) {
			return false
		}
	}
	return true
}

func isTimeComponent(s string) bool {
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" || strings.ContainsFunc(p, func(r rune) bool {
			return !strings.ContainsRune(calendarChars, r)
		}) {
			return false
		}
	}
	return true
}

func scheduleError(field, value string) error {
	return fmt.Errorf("%q is not a %s servlo can read: %s", value, field, scheduleHelp)
}
