package systemd

import "strings"

// EscapeSpecifiers doubles every per cent sign so systemd hands the command on
// as the operator typed it.
//
// A per cent sign in a unit is a specifier, resolved before anything runs, and
// doubling it is how a unit says it meant the character. Which way it goes
// wrong depends on the letter that follows, because nearly every letter is one:
// an unknown specifier fails the unit outright, and a known one substitutes
// quietly, so %h in a log format string becomes the home directory and nobody
// is told.
//
// Commands reach a unit from the operator, in a site's cron entries, in a
// custom worker, and in the command a host-proxy site is served by, and a per
// cent sign in any of them is ordinary: a date in a filename, an access log
// format, a printf.
func EscapeSpecifiers(command string) string {
	return strings.ReplaceAll(command, "%", "%%")
}
