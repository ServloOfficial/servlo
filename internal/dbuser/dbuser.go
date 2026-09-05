// Package dbuser issues and rotates the account a site reaches its database as.
//
// All sites on a servlo server run as the same Linux user, which is a
// documented tradeoff (PRD §6). It stops being a tradeoff and starts being a
// hole if they also share a database account: a site's own code, or anything
// that gets into it, could then read and drop every other site's data. Each
// site gets its own account, granted on its own schemas.
//
// Nothing here knows any SQL. The statements are declared by the engine's
// preset, under an entity of kind site_users, in the engine's own dialect,
// including how its client spells TLS. What this package supplies is the four
// values a statement cannot know — which account, which password, which
// database, and which server — and a place to run the client: inside the
// container for a database servlo runs, and on the servlo network aimed at the
// provider for one it does not.
package dbuser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbcred"
	"github.com/ServloOfficial/servlo/internal/dbexec"
	"github.com/ServloOfficial/servlo/internal/podman"
	"github.com/ServloOfficial/servlo/internal/serviceops"
)

// siteUsersKind is the entity a database preset declares its per-site account
// statements under.
const siteUsersKind = "site_users"

// commandTimeout bounds one statement. Creating a user and granting on a schema
// are both quick; this is here so an unreachable managed host reports rather
// than holding a panel request open.
const commandTimeout = dbexec.DefaultTimeout

// runCommand is the seam. Every test in this package asserts on the argv that
// would have run, because what matters about provisioning an account is the
// statement and where it was aimed.
var runCommand = func(args []string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := podman.CmdContext(ctx, args...)
	cmd.Env = append(cmd.Environ(), env...)
	return cmd.CombinedOutput()
}

// errUnsupported marks an engine that declares no per-site accounts. It is a
// sentinel rather than a message because every caller does the same thing with
// it: leave the site reaching its database the way it already does, and say so
// once rather than failing the run.
var errUnsupported = errors.New("this engine declares no per-site database accounts")

// Unsupported reports whether err is an engine that cannot issue accounts.
func Unsupported(err error) bool { return errors.Is(err, errUnsupported) }

// Ensure makes sure database's own account exists on conn and is granted on
// each of grants, and returns the credential to write into the site's env file.
//
// It is safe to run on every `servlo env`. An account that is already there
// keeps the password it already has: replacing it would lock out the running
// application, which is holding the old one until its next reload.
func Ensure(conn dbconn.Connection, database string, grants []string) (dbconn.SiteUser, error) {
	spec, err := specFor(conn)
	if err != nil {
		return dbconn.SiteUser{}, err
	}
	return ensureWith(spec, conn, database, grants)
}

func ensureWith(spec *config.EntitySpec, conn dbconn.Connection, database string, grants []string) (dbconn.SiteUser, error) {
	if len(grants) == 0 {
		grants = []string{database}
	}
	cred, ok := dbcred.For(conn, database)
	if !ok {
		password, err := dbcred.GeneratePassword()
		if err != nil {
			return dbconn.SiteUser{}, err
		}
		cred = dbconn.SiteUser{Connection: dbcred.Key(conn), Database: database, User: dbcred.UserName(database), Password: password}
	}

	// create is declared IF NOT EXISTS, so this both provisions a new account
	// and heals one an operator dropped by hand back to the password servlo
	// holds. Running it every time is what keeps the store and the server from
	// drifting apart quietly.
	if err := run(spec, conn, "create", cred, database); err != nil {
		return dbconn.SiteUser{}, err
	}
	for _, schema := range grants {
		if err := run(spec, conn, "grant", cred, schema); err != nil {
			return dbconn.SiteUser{}, err
		}
	}
	if err := dbcred.Record(conn, database, cred.User, cred.Password); err != nil {
		return dbconn.SiteUser{}, err
	}
	return cred, nil
}

// Rotate issues a new password for a site's account.
//
// The server is changed first and the store second. A store that moved on
// without the server is a site locked out of its own database with nothing to
// say why, so on a refusal the old password stays where it is and the error
// comes back.
//
// A site that has no account yet gets one, because rotating is also how an
// operator moves a site created before per-site accounts existed off the
// administrator.
func Rotate(conn dbconn.Connection, database string) (dbconn.SiteUser, error) {
	spec, err := specFor(conn)
	if err != nil {
		return dbconn.SiteUser{}, err
	}
	if _, ok := dbcred.For(conn, database); !ok {
		return ensureWith(spec, conn, database, nil)
	}
	password, err := dbcred.GeneratePassword()
	if err != nil {
		return dbconn.SiteUser{}, err
	}
	cred := dbconn.SiteUser{
		Connection: dbcred.Key(conn),
		Database:   database,
		User:       dbcred.UserName(database),
		Password:   password,
	}
	if err := run(spec, conn, "rotate", cred, database); err != nil {
		return dbconn.SiteUser{}, err
	}
	if err := dbcred.Record(conn, database, cred.User, cred.Password); err != nil {
		return dbconn.SiteUser{}, err
	}
	return cred, nil
}

// specFor resolves the statements for the engine behind a connection.
//
// A local service answers for itself. A managed database has no service to ask,
// so the statements come from a preset of the same dialect: the engine on the
// other end speaks MySQL or PostgreSQL, and that is the whole of what the
// statements depend on.
func specFor(c dbconn.Connection) (*config.EntitySpec, error) {
	if c.Service != "" {
		if spec := serviceops.EntityFor(c.Service, siteUsersKind); spec != nil {
			return spec, nil
		}
	}
	dialect := dbconn.Dialect(c.Family)
	if dialect == "" {
		dialect = dbconn.DialectForService(c.Service)
	}
	if spec := presetSpecForDialect(dialect); spec != nil {
		return spec, nil
	}
	where := c.Name
	if where == "" {
		where = c.Service
	}
	return nil, fmt.Errorf("%s: %w", where, errUnsupported)
}

// presetSpecForDialect finds a preset that declares the statements for a
// dialect. The exact family is preferred so a MySQL connection resolves MySQL's
// statements rather than MariaDB's when both are on disk, and the rest are
// sorted so the answer does not depend on directory order.
func presetSpecForDialect(dialect string) *config.EntitySpec {
	if dialect == "" {
		return nil
	}
	metas, err := config.ListPresets()
	if err != nil {
		return nil
	}
	var fallback *config.EntitySpec
	for _, meta := range metas {
		p, err := config.LoadPreset(meta.Name)
		if err != nil || p.Introspect == nil {
			continue
		}
		spec := p.Introspect.Entity(siteUsersKind)
		if spec == nil || dbconn.Dialect(p.Family) != dialect {
			continue
		}
		if p.Family == dialect {
			return spec
		}
		if fallback == nil {
			fallback = spec
		}
	}
	return fallback
}

// run expands one declared statement and executes it.
//
// The account name and its password are the two values only this package can
// supply; everything else about where the statement runs and what it may be
// handed is dbexec's, shared with the dump and provisioning paths so the
// injection guard has one home rather than three.
func run(spec *config.EntitySpec, conn dbconn.Connection, action string, cred dbconn.SiteUser, database string) error {
	act, ok := spec.Actions[action]
	if !ok {
		return fmt.Errorf("this engine declares no %s for a site's database account", action)
	}
	vars, err := dbexec.Vars(spec, conn, map[string]string{
		// Not {{password}}: that placeholder is replaced across a preset's raw
		// bytes with this install's service password before the definition is
		// even parsed, so a statement using it would set every site's account
		// to the administrator's password.
		"name":          cred.User,
		"user_password": cred.Password,
		"database":      database,
	})
	if err != nil {
		return err
	}
	shellCmd, err := dbexec.Expand(act.Exec, vars)
	if err != nil {
		return err
	}
	args, err := dbexec.CommandArgs(spec, conn, shellCmd)
	if err != nil {
		return err
	}
	// The credentials travel in the environment, because CommandArgs forwards
	// them into the container by name rather than spelling them in the argv.
	out, err := runCommand(args, conn.ClientEnv())
	if err != nil {
		return fmt.Errorf("%s the database account for %s: %w\n%s", action, database, err,
			dbexec.Redact(strings.TrimSpace(string(out)), cred.Password, conn.Password))
	}
	return nil
}

// Address is where a connection answers, for a panel that shows an operator
// which server an account lives on without showing what opens it.
func Address(conn dbconn.Connection) string {
	if conn.Local() {
		return "servlo-" + conn.Service
	}
	return net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port))
}
