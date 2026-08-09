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
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbcred"
	"github.com/realrashid/servlo/internal/podman"
	"github.com/realrashid/servlo/internal/serviceops"
)

// siteUsersKind is the entity a database preset declares its per-site account
// statements under.
const siteUsersKind = "site_users"

// containerCACert is where a managed connection's CA certificate is mounted
// inside the container running the client, so the declared flags have a fixed
// path to name.
const containerCACert = "/etc/servlo/db-ca.crt"

// commandTimeout bounds one statement. Creating a user and granting on a schema
// are both quick; this is here so an unreachable managed host reports rather
// than holding a panel request open.
const commandTimeout = 60 * time.Second

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
func run(spec *config.EntitySpec, conn dbconn.Connection, action string, cred dbconn.SiteUser, database string) error {
	act, ok := spec.Actions[action]
	if !ok {
		return fmt.Errorf("this engine declares no %s for a site's database account", action)
	}
	vars, err := statementVars(spec, conn, cred, database)
	if err != nil {
		return err
	}
	shellCmd, err := expand(act.Exec, vars)
	if err != nil {
		return err
	}
	args, err := commandArgs(spec, conn, shellCmd)
	if err != nil {
		return err
	}
	out, err := runCommand(args, nil)
	if err != nil {
		return fmt.Errorf("%s the database account for %s: %w\n%s", action, database, err,
			redact(strings.TrimSpace(string(out)), cred.Password, conn.Password))
	}
	return nil
}

// statementVars is everything a declared statement can ask for.
//
// A local database is addressed as 127.0.0.1 because the client runs inside the
// engine's own container; a managed one by the host the provider gave. The TLS
// flags are the preset's, keyed by the mode the connection asks for, so the
// engine's own spelling stays in the store.
func statementVars(spec *config.EntitySpec, conn dbconn.Connection, cred dbconn.SiteUser, database string) (map[string]string, error) {
	host := conn.Host
	if conn.Local() {
		host = "127.0.0.1"
	}
	vars := map[string]string{
		"name": cred.User,
		// Not {{password}}: that placeholder is replaced across a preset's raw
		// bytes with this install's service password before the definition is
		// even parsed, so a statement using it would set every site's account
		// to the administrator's password.
		"user_password": cred.Password,
		"database":      database,
		"host":          host,
		"port":          strconv.Itoa(conn.Port),
		"admin_user":    conn.User,
		"ca_cert":       containerCACert,
		"tls_flags":     "",
	}
	if !conn.Local() && conn.TLSMode != dbconn.TLSOff {
		flags, declared := spec.TLS[conn.TLSMode]
		if !declared {
			return nil, fmt.Errorf("connection %q asks for TLS mode %q, which this engine's definition does not say how to spell", conn.Name, conn.TLSMode)
		}
		// The flags are the one value that may itself name another: verify-ca
		// has to point the client at where the certificate was mounted.
		vars["tls_flags"] = strings.ReplaceAll(flags, "{{ca_cert}}", containerCACert)
	}
	return vars, nil
}

// commandArgs is where the client runs: inside the engine's container for a
// database servlo runs, and ephemerally on the servlo network for one it does
// not. The administrator's password goes in through the environment either way,
// so it is never in an argument list the rest of the machine can read.
func commandArgs(spec *config.EntitySpec, conn dbconn.Connection, shellCmd string) ([]string, error) {
	if conn.Local() {
		args := []string{"exec"}
		for _, kv := range conn.ClientEnv() {
			args = append(args, "--env", kv)
		}
		return append(args, "servlo-"+conn.Service, "sh", "-c", shellCmd), nil
	}
	if strings.TrimSpace(spec.Image) == "" {
		return nil, fmt.Errorf("this engine's definition names no client image, so there is nothing to run the statement in for a database servlo does not host")
	}
	// --entrypoint sh for the same reason the entity runner does it: an engine
	// image makes its server or its client the entrypoint, which would swallow
	// the command as its own arguments.
	args := []string{"run", "--rm", "--network", "servlo", "--entrypoint", "sh"}
	for _, kv := range conn.ClientEnv() {
		args = append(args, "-e", kv)
	}
	if conn.TLSMode == dbconn.TLSVerifyCA && conn.CACert != "" {
		args = append(args, "-v", conn.CACert+":"+containerCACert+":ro")
	}
	return append(args, spec.Image, "-c", shellCmd), nil
}

// Values a declared statement may be given, and what each may contain.
//
// This is the injection guard, and it is a whitelist rather than an escape:
// every value here is either generated by servlo or read from a connection the
// operator configured, and none of them has any business carrying a quote, a
// backtick or a shell metacharacter. A value that does is a bug somewhere
// earlier, and running it would be running whatever the bug wrote.
var valuePatterns = map[string]*regexp.Regexp{
	"name":          regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`),
	"user_password": regexp.MustCompile(`^[A-Za-z0-9]{24,128}$`),
	"database":      regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`),
	"host":          regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`),
	"port":          regexp.MustCompile(`^[1-9][0-9]{0,4}$`),
	"admin_user":    regexp.MustCompile(`^[A-Za-z0-9_.-]{1,63}$`),
	"ca_cert":       regexp.MustCompile(`^[A-Za-z0-9._/-]{0,255}$`),
	// The TLS flags come from the preset itself rather than from anything a
	// user typed, so what is checked is that expansion left nothing behind.
	"tls_flags": regexp.MustCompile(`^[A-Za-z0-9=?&_.:/ -]*$`),
}

// expand fills a declared statement's placeholders, refusing any value that is
// not what its placeholder is allowed to hold.
func expand(command string, vars map[string]string) (string, error) {
	out := strings.TrimSpace(command)
	for key, value := range vars {
		pattern, known := valuePatterns[key]
		if !known {
			return "", fmt.Errorf("%q is not a value a database account statement can take", key)
		}
		if !pattern.MatchString(value) {
			return "", fmt.Errorf("%q is not a usable %s for a database account statement", value, key)
		}
		out = strings.ReplaceAll(out, "{{"+key+"}}", value)
	}
	return out, nil
}

// redact keeps a client that echoes its arguments out of the error, the audit
// log and every screenshot of either.
func redact(text string, secrets ...string) string {
	for _, s := range secrets {
		if len(s) >= 8 {
			text = strings.ReplaceAll(text, s, "****")
		}
	}
	return text
}

// Address is where a connection answers, for a panel that shows an operator
// which server an account lives on without showing what opens it.
func Address(conn dbconn.Connection) string {
	if conn.Local() {
		return "servlo-" + conn.Service
	}
	return net.JoinHostPort(conn.Host, strconv.Itoa(conn.Port))
}
