// Package dbconn answers one question: how does servlo reach a database.
//
// A database is a connection, not a container (PRD §5.9). Today every
// connection servlo knows about is a service it runs itself, and this package
// says where that service lives and what credentials open it; an external
// managed database is the same question with different answers, which is why
// the answer is a value rather than a constant in each caller.
//
// It exists because it was not a value. The presets moved to a password
// generated per install, so a database container has a different credential on
// every machine, but the Go that reaches into those containers kept passing the
// literal the presets used to ship. Listing databases, taking a snapshot,
// restoring one, importing a dump, opening a shell and migrating an engine were
// all authenticating with a password no server had. One place to ask means the
// next caller cannot get it wrong by writing it down.
package dbconn

import (
	"fmt"
	"strings"

	"github.com/realrashid/servlo/internal/config"
)

// Connection is where a database lives and how to reach it.
type Connection struct {
	// Family is the engine dialect: "mysql" or "postgres". Not the service
	// name, because mariadb and mysql-8-4 speak the same protocol and take the
	// same client.
	Family string
	// Service is the servlo service holding the database, empty when the
	// database is somewhere servlo does not run.
	Service string
	// Host and Port reach the database from outside its own container.
	Host string
	Port int
	// User is the administrative account servlo provisions with.
	User string
	// Password is what opens that account.
	Password string
}

// Local reports whether this database is one servlo runs.
func (c Connection) Local() bool { return c.Service != "" }

// familyUser is the administrative account each engine ships with. Two
// engines, two names, and no third case: an engine servlo cannot name an admin
// for is one it cannot provision either.
func familyUser(family string) string {
	if family == "postgres" {
		return "postgres"
	}
	return "root"
}

// familyPort is the port an engine answers on inside the container network.
func familyPort(family string) int {
	if family == "postgres" {
		return 5432
	}
	return 3306
}

// ForService is the connection to a database servlo runs itself.
//
// The password is this install's generated service password. It is read rather
// than remembered, because it is written once at install and a value cached in
// a long-running panel would survive the operator rotating it.
func ForService(service string) (Connection, error) {
	family := config.FamilyOfName(service)
	return forFamilyService(family, service)
}

// ForFamily is the connection to the canonical local service of a family, for
// callers that know the dialect but not which service is in front of them.
func ForFamily(family string) (Connection, error) {
	return forFamilyService(family, "")
}

// forFamilyService fills in everything it can and reports separately whether
// the password came with it. A caller that only needs the address should not
// have to hold an unreadable secret file against it, and one that does need the
// password gets an error saying so rather than an empty string.
func forFamilyService(family, service string) (Connection, error) {
	c := Connection{
		Family:  family,
		Service: service,
		Port:    familyPort(family),
		User:    familyUser(family),
	}
	if service != "" {
		c.Host = "servlo-" + service
	}
	password, err := config.ServicePassword()
	if err != nil {
		return c, fmt.Errorf("reading this install's service password: %w", err)
	}
	c.Password = password
	return c, nil
}

// ClientEnv is the environment a command-line client reads its password from,
// so the credential never appears in a process's arguments where every other
// user on the machine can read it out of the process list.
//
// Both variables when the family is not known: an engine ignores the one that
// is not its own, and a caller running a command inside a container it only
// knows the name of should not have to guess which client will read it.
func (c Connection) ClientEnv() []string {
	switch c.Family {
	case "postgres":
		return []string{"PGPASSWORD=" + c.Password}
	case "mysql":
		return []string{"MYSQL_PWD=" + c.Password}
	default:
		return []string{"MYSQL_PWD=" + c.Password, "PGPASSWORD=" + c.Password}
	}
}

// URL is the connection string for this database, for a client servlo hands to
// somebody rather than runs itself.
func (c Connection) URL(database string) string {
	scheme := "mysql"
	if c.Family == "postgres" {
		scheme = "postgresql"
	}
	host := c.Host
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("%s://%s:%s@%s:%d/%s", scheme, c.User, c.Password, host, c.Port, strings.TrimSpace(database))
}
