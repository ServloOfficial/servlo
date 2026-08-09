// Package dbdump streams a database out, wherever that database lives.
//
// A backup of a site whose data is on a managed provider has to carry that
// data, and the existing export path cannot reach it: it runs the dump inside
// servlo-<service>, and a managed database has no container here. So a dump is
// two statements rather than one, both the engine's own — the local one it
// already declared, and a remote one spelling the host, port, user and TLS
// flags — and this package picks between them by where the connection is.
//
// Nothing here knows any SQL, and nothing here knows an engine's name.
package dbdump

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/realrashid/servlo/internal/config"
	"github.com/realrashid/servlo/internal/dbconn"
	"github.com/realrashid/servlo/internal/dbexec"
	"github.com/realrashid/servlo/internal/serviceops"
)

// databasesKind is the entity a database preset declares its dump under, the
// same one the panel's export already uses.
const databasesKind = "databases"

// Actions a definition may declare. A local dump runs where the engine is and
// needs no address; a remote one is aimed, and is a separate declaration
// because the flags differ per engine and belong in the store.
const (
	localExport  = "export"
	remoteExport = "remote_export"
)

// Timeout bounds one dump. Far longer than dbexec's default, because a large
// database legitimately takes a while and cutting it off would write a
// truncated dump into an archive that claims to hold a whole one.
const Timeout = 6 * time.Hour

// Dump streams the dump of database on conn to w.
func Dump(conn dbconn.Connection, database string, w io.Writer) error {
	spec, err := specFor(conn)
	if err != nil {
		return err
	}
	return dumpWith(spec, conn, database, w)
}

func dumpWith(spec *config.EntitySpec, conn dbconn.Connection, database string, w io.Writer) error {
	action := remoteExport
	if conn.Local() {
		action = localExport
	}
	act, ok := spec.Actions[action]
	if !ok {
		where := "a database servlo does not host"
		if conn.Local() {
			where = "this engine"
		}
		return fmt.Errorf("this engine's definition declares no %s, so servlo cannot dump %s on %s",
			action, database, where)
	}

	// Both names, one value. The local export the panel already uses spells the
	// database as {{name}}, because to that entity it is the name of the thing
	// being exported; the aimed statement spells it {{database}}, because it
	// also has a host and a user to keep apart from it.
	vars, err := dbexec.Vars(spec, conn, map[string]string{
		"name":     database,
		"database": database,
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

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	// The dump goes straight to w and only what the engine said on stderr comes
	// back, so a multi-gigabyte database is never held in memory.
	out, err := dbexec.Run(ctx, args, conn.ClientEnv(), w)
	if err != nil {
		return fmt.Errorf("dumping %s: %w\n%s", database, err,
			dbexec.Redact(strings.TrimSpace(string(out)), conn.Password))
	}
	return nil
}

// specFor finds the definition that says how to dump this connection's engine.
func specFor(conn dbconn.Connection) (*config.EntitySpec, error) {
	if conn.Service != "" {
		if spec := serviceops.EntityFor(conn.Service, databasesKind); spec != nil {
			return spec, nil
		}
	}
	dialect := dbconn.Dialect(conn.Family)
	if dialect == "" {
		dialect = dbconn.DialectForService(conn.Service)
	}
	if spec := presetSpecForDialect(dialect); spec != nil {
		return spec, nil
	}
	where := conn.Name
	if where == "" {
		where = conn.Service
	}
	return nil, fmt.Errorf("%s: servlo has no definition saying how to dump this engine", where)
}

// presetSpecForDialect finds a preset declaring databases for a dialect. The
// exact family is preferred so a MySQL connection resolves MySQL's statements
// rather than MariaDB's when both are on disk, and the rest are sorted so the
// answer does not depend on directory order.
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
		spec := p.Introspect.Entity(databasesKind)
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

// Actions for loading a dump back. Same split as the dump: the local one runs
// where the engine is, the aimed one is for a database servlo does not host.
const (
	localImport  = "import"
	remoteImport = "remote_import"
)

// Load streams a dump from r into database on conn.
//
// The mirror of Dump, and it picks between the engine's two declared statements
// the same way. A load that fails is reported rather than swallowed: a restore
// that wrote the files and quietly failed on the data leaves a site running
// against whatever was in the database before, which looks like it worked.
func Load(conn dbconn.Connection, database string, r io.Reader) error {
	spec, err := specFor(conn)
	if err != nil {
		return err
	}
	return loadWith(spec, conn, database, r)
}

func loadWith(spec *config.EntitySpec, conn dbconn.Connection, database string, r io.Reader) error {
	action := remoteImport
	if conn.Local() {
		action = localImport
	}
	act, ok := spec.Actions[action]
	if !ok {
		return fmt.Errorf("this engine's definition declares no %s, so servlo cannot load %s back", action, database)
	}
	vars, err := dbexec.Vars(spec, conn, map[string]string{"name": database, "database": database})
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
	// The client has to be given a stdin to read the dump from, which is what
	// the interactive flag on the run means here.
	args = dbexec.WithStdin(args)

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	out, err := dbexec.RunWithInput(ctx, args, conn.ClientEnv(), r)
	if err != nil {
		return fmt.Errorf("loading %s: %w\n%s", database, err,
			dbexec.Redact(strings.TrimSpace(string(out)), conn.Password))
	}
	return nil
}
