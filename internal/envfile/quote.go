package envfile

import "strings"

// Quoting a value on its way into a .env file.
//
// What a value may carry bare was measured against phpdotenv 5.7, which is what
// Laravel, Statamic, Tempest, CodeIgniter and Grav read these files with. Only
// three characters are not allowed through: a space or a tab makes it refuse
// the whole file, and a hash truncates the value there with nothing said.
//
// Refusing the file is the one that hurts. It is not the setting failing, it is
// the framework unable to boot, so every page of the site is a 500 from the
// moment the operator pressed Save on the mail form. And they will: an SMTP
// password from Google is four groups separated by spaces, and a from name is a
// company's.
//
// Everything else stays bare, which is what keeps this from rewriting every
// .env on the machine the first time servlo touches one.
const dotenvMustQuote = " \t#"

// quoteValue renders v as the right-hand side of a .env line.
//
// Single quotes where they will do, because phpdotenv takes a single-quoted
// value literally: a dollar, a backslash and a double quote all arrive as
// themselves. Only a value carrying a single quote needs the other kind, and
// there the dollar has to be escaped too or the framework substitutes a
// variable into the operator's password.
func quoteValue(v string) string {
	if !strings.ContainsAny(v, dotenvMustQuote) {
		return v
	}
	if !strings.Contains(v, "'") {
		return "'" + v + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`)
	return `"` + r.Replace(v) + `"`
}

// unquoteValue is the inverse, for reading a value back out.
//
// A file servlo did not write is read the same way, which is the point: an
// operator who quoted a value by hand gets it back without the quotes, and one
// who did not is unaffected.
func unquoteValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 || v[0] != v[len(v)-1] || (v[0] != '\'' && v[0] != '"') {
		return strings.Trim(v, `"'`)
	}
	inner := v[1 : len(v)-1]
	if v[0] == '\'' {
		return inner
	}
	r := strings.NewReplacer(`\\`, `\`, `\"`, `"`, `\$`, `$`)
	return r.Replace(inner)
}
