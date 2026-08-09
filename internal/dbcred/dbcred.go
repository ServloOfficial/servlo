// Package dbcred is where a site's own database account is looked up.
//
// Every site used to reach its database as the engine's administrator, which
// meant one site's code could read, and drop, every other site's data. Servlo
// runs all sites as one Linux user by design (PRD §6), so the database account
// is one of the two places left where a site can be fenced off from its
// neighbours: its own user, granted on its own schema and nothing else.
//
// The accounts themselves live in the connection registry, beside the
// connections they belong to, because that file is already the 0600 place a
// database credential goes. This package is the leaf view of them: what an
// account is called, what opens it, and which connection it is on. It is a leaf
// so that placeholder expansion can read a credential without dragging in
// podman, the service store or the provisioning path — creating one lives in
// internal/dbuser, which is free to depend on all three.
package dbcred

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"

	"github.com/realrashid/servlo/internal/dbconn"
)

// Key is what a connection is called in the account store.
//
// A named connection by its name. A local service resolved without a registry
// entry has no name at all, and its accounts still have to be found again on
// the next run, so the service standing in for it is the key. Both are stable
// for as long as the site is on that database, which is what a key has to be.
func Key(c dbconn.Connection) string {
	if c.Name != "" {
		return c.Name
	}
	return c.Service
}

// For returns the account a database holds on a connection.
//
// A miss is the ordinary state of every site created before per-site accounts
// existed, so it is reported rather than raised: the caller falls back to the
// connection's administrator, which is exactly what such a site is using today.
func For(c dbconn.Connection, database string) (dbconn.SiteUser, bool) {
	key := Key(c)
	if key == "" || database == "" {
		return dbconn.SiteUser{}, false
	}
	user, ok := dbconn.SiteUserFor(key, database)
	if !ok || !ValidUser(user.User) || user.Password == "" {
		return dbconn.SiteUser{}, false
	}
	return user, true
}

// Record writes an account down against a connection.
func Record(c dbconn.Connection, database, user, password string) error {
	key := Key(c)
	if key == "" {
		return fmt.Errorf("cannot record a database account against a connection with no name")
	}
	return dbconn.RecordSiteUser(key, database, user, password)
}

// validUser is the shape an account name must have to be spliced into the SQL
// an engine's preset declares: a leading letter, then letters, digits and
// underscores. Every SQL and shell metacharacter is excluded, which is what
// makes the splice safe rather than the quoting around it.
var validUser = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// validPassword is the same guarantee for a password. Servlo generates these
// itself, from an alphabet with no quote, backslash or shell character in it.
var validPassword = regexp.MustCompile(`^[A-Za-z0-9]{24,128}$`)

// ValidUser reports whether name is safe to splice into a declared statement.
func ValidUser(name string) bool { return validUser.MatchString(name) }

// ValidPassword reports whether password is one servlo could have generated,
// and so safe to splice into a declared statement.
func ValidPassword(password string) bool { return validPassword.MatchString(password) }

// UserName is what the account for a database is called.
//
// The database's own name where it fits, so an operator reading the engine's
// user list can tell whose account each one is. The truncation, and the digest
// that keeps two long names apart, is dbconn's: a managed database provisioned
// there and a local one provisioned here have to arrive at the same name for
// the same site, or a site that moves between them acquires two accounts.
func UserName(database string) string {
	return dbconn.SiteUserName(sanitise(database))
}

// sanitise turns a database handle into an identifier. Anything that is not a
// letter, digit or underscore becomes one, and a name that would not start with
// a letter is prefixed, because both engines refuse a bare leading digit
// without quoting.
func sanitise(database string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(database) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "site"
	}
	if out[0] < 'a' || out[0] > 'z' {
		out = "s_" + out
	}
	return out
}

// passwordAlphabet is letters and digits only. A password is spliced into the
// engine's own CREATE USER statement and then written into a site's env file,
// and an alphabet with no quote or backslash in it means neither splice can be
// broken out of. 28 characters of it is around 166 bits, which is more than a
// symbol set would buy back.
const passwordAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

const passwordLength = 28

// GeneratePassword returns a new site password.
func GeneratePassword() (string, error) {
	buf := make([]byte, passwordLength)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating a database password: %w", err)
	}
	for i, b := range buf {
		buf[i] = passwordAlphabet[int(b)%len(passwordAlphabet)]
	}
	return string(buf), nil
}
