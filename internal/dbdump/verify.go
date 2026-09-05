package dbdump

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbexec"
)

// The declared statements a verify needs. All aimed, because the same three
// work for a database servlo hosts and one it does not: for a local connection
// the client runs in the engine's own container and the address resolves to
// 127.0.0.1, which is the server it is standing on.
const (
	remoteCreate = "remote_create"
	remoteDrop   = "remote_drop"
	remoteCount  = "remote_count_tables"
)

// Result is what a verify found.
type Result struct {
	// Scratch is the database it restored into, already dropped by the time
	// this is returned.
	Scratch string
	// Tables is how many the dump created. Zero is a failure, not a result: a
	// dump that restores into nothing is exactly what this exists to catch.
	Tables int
}

// ScratchName is a database to restore into and then throw away.
//
// It says what it is, so an operator who finds one left behind by a killed
// process knows it is safe to drop, and it carries random bytes so two verifies
// running at once cannot drop each other's.
func ScratchName(site string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	name := "servlo_verify_" + config.SiteSlug(site) + "_" + hex.EncodeToString(b[:])
	if len(name) > 64 {
		// The tail is the random part and the only bit that has to survive
		// intact, since it is what keeps two verifies apart.
		name = name[:55] + hex.EncodeToString(b[:])
	}
	return name
}

// Verify restores a dump into a scratch database, counts what arrived, and
// takes the scratch database away again.
//
// This is the whole point of the backup story. Most panels ship a green tick
// that means a file was written, which tells you nothing about whether it can
// be restored, and the day you find out is the day it matters.
func Verify(conn dbconn.Connection, scratch string, dump io.Reader) (Result, error) {
	spec, err := specFor(conn)
	if err != nil {
		return Result{}, err
	}
	return verifyWith(spec, conn, scratch, dump)
}

func verifyWith(spec *config.EntitySpec, conn dbconn.Connection, scratch string, dump io.Reader) (Result, error) {
	res := Result{Scratch: scratch}

	if err := runVerifyAction(spec, conn, remoteCreate, scratch); err != nil {
		return res, err
	}
	// From here on the scratch database exists, so every path out drops it.
	// Leaving one behind per failed check would fill the server with
	// half-restored copies of the thing that was already failing.
	defer func() {
		_ = runVerifyAction(spec, conn, remoteDrop, scratch)
	}()

	if err := loadWith(spec, conn, scratch, dump); err != nil {
		return res, fmt.Errorf("the dump would not load: %w", err)
	}

	tables, err := countTables(spec, conn, scratch)
	if err != nil {
		return res, err
	}
	res.Tables = tables
	if tables == 0 {
		return res, fmt.Errorf("the dump restored without error but left no tables behind, so it is not a backup of anything")
	}
	return res, nil
}

// countTables asks the engine how much arrived. A number rather than a checksum
// because what is being checked is that the dump is a real one, not that it
// matches a particular state: the live database has moved on since it was
// taken, and comparing against it would fail every time.
func countTables(spec *config.EntitySpec, conn dbconn.Connection, database string) (int, error) {
	act, ok := spec.Actions[remoteCount]
	if !ok {
		return 0, fmt.Errorf("this engine's definition declares no %s, so servlo cannot check what a restore brought back", remoteCount)
	}
	var out strings.Builder
	if err := runAimed(spec, conn, act.Exec, database, &out); err != nil {
		return 0, err
	}
	text := strings.TrimSpace(out.String())
	// Engines pad and label differently; the count is the first bare number.
	for _, field := range strings.Fields(text) {
		if n, err := strconv.Atoi(strings.TrimSpace(field)); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("the engine answered %q when asked how many tables the restore brought back", text)
}

func runVerifyAction(spec *config.EntitySpec, conn dbconn.Connection, action, database string) error {
	act, ok := spec.Actions[action]
	if !ok {
		return fmt.Errorf("this engine's definition declares no %s, so servlo cannot verify a backup on it", action)
	}
	return runAimed(spec, conn, act.Exec, database, nil)
}

// runAimed expands and runs one declared statement against the connection,
// optionally capturing what it printed.
func runAimed(spec *config.EntitySpec, conn dbconn.Connection, exec, database string, out io.Writer) error {
	vars, err := dbexec.Vars(spec, conn, map[string]string{"name": database, "database": database})
	if err != nil {
		return err
	}
	shellCmd, err := dbexec.Expand(exec, vars)
	if err != nil {
		return err
	}
	args, err := dbexec.CommandArgs(spec, conn, shellCmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	stderr, err := dbexec.Run(ctx, args, conn.ClientEnv(), out)
	if err != nil {
		return fmt.Errorf("%w\n%s", err, dbexec.Redact(strings.TrimSpace(string(stderr)), conn.Password))
	}
	return nil
}
