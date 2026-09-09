package serviceops

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
)

// Snapshot is the meta.json sidecar describing one stored database snapshot.
type Snapshot struct {
	Name         string    `json:"name"`
	Created      time.Time `json:"created"`
	Service      string    `json:"service"`
	Family       string    `json:"family"`
	Database     string    `json:"database"`
	AllDatabases bool      `json:"all_databases"`
	DumpFile     string    `json:"dump_file"`
	Compressed   bool      `json:"compressed"`
	SizeBytes    int64     `json:"size_bytes"`
	Site         string    `json:"site,omitempty"`
	GitBranch    string    `json:"git_branch,omitempty"`
}

// SnapshotTarget identifies the live database a snapshot is taken from or
// restored into. The cli layer builds it from a resolved DB env.
type SnapshotTarget struct {
	Service      string
	Family       string
	Database     string
	AllDatabases bool
}

// SnapshotMeta carries the optional best-effort context recorded into a
// snapshot's meta.json.
type SnapshotMeta struct {
	Site      string
	GitBranch string
}

const (
	snapshotDumpFile = "dump.sql.gz"
	snapshotMetaFile = "meta.json"
	snapshotDBScope  = "databases"
	snapshotAllScope = "all"
)

// reservedSnapshotName flags snapshot names that collide with command verbs,
// catching mistakes like `servlo db snapshot list` (which would otherwise create
// a snapshot literally named "list" instead of listing).
func reservedSnapshotName(name string) (hint string, reserved bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "list", "ls", "snapshots":
		return "did you mean `servlo db:snapshots` to list snapshots?", true
	case "rm", "remove", "delete", "del":
		return "use `servlo db:snapshot:rm <name>` to delete a snapshot", true
	case "restore":
		return "use `servlo db:restore <name>` to restore a snapshot", true
	}
	return "", false
}

// sanitizeSnapshotName validates a user-supplied snapshot name so it is safe to
// use as a single filesystem path component.
func sanitizeSnapshotName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("snapshot name cannot be empty")
	case name == "." || name == "..":
		return "", fmt.Errorf("invalid snapshot name %q", name)
	case strings.HasPrefix(name, "."):
		return "", fmt.Errorf("snapshot name cannot start with a dot: %q", name)
	case strings.ContainsAny(name, `/\`):
		return "", fmt.Errorf("snapshot name cannot contain path separators: %q", name)
	}
	return name, nil
}

// snapshotScopeDir returns the directory holding every snapshot for a given
// scope. When allDatabases is set the database argument is ignored and the
// service-wide scope is used.
func snapshotScopeDir(service, database string, allDatabases bool) string {
	if allDatabases {
		return filepath.Join(config.SnapshotsDir(), service, snapshotAllScope)
	}
	return filepath.Join(config.SnapshotsDir(), service, snapshotDBScope, database)
}

// snapshotDir returns the directory for one named snapshot.
func snapshotDir(service, database, name string, allDatabases bool) string {
	return filepath.Join(snapshotScopeDir(service, database, allDatabases), name)
}

func writeSnapshotMeta(dir string, s Snapshot) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, snapshotMetaFile), data, 0600)
}

// readSnapshotMeta loads a snapshot's meta.json. The os.ReadFile error is
// returned unwrapped so callers can test a missing snapshot with os.IsNotExist.
func readSnapshotMeta(dir string) (Snapshot, error) {
	var s Snapshot
	data, err := os.ReadFile(filepath.Join(dir, snapshotMetaFile))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return s, fmt.Errorf("parsing %s: %w", snapshotMetaFile, err)
	}
	return s, nil
}

// snapshotEnv returns the podman exec env pairs carrying the container admin
// password for the target family.
func snapshotEnv(family string) []string {
	c, err := dbconn.ForFamily(family)
	if err != nil {
		return nil
	}
	return c.ClientEnv()
}

// snapshotDumpCommand builds the in-container shell command that writes a
// gzipped dump to stdout for the given target, wrapping the export action the
// engine's preset declares (export_all for a service-wide snapshot, which is
// self-cleaning so a restore replaces each contained database).
func snapshotDumpCommand(t SnapshotTarget) (string, error) {
	spec := EntityFor(t.Service, "databases")
	if t.AllDatabases {
		act, ok := entityAction(spec, "export_all")
		if !ok {
			return "", fmt.Errorf("service-wide snapshots are not supported for %q", t.Service)
		}
		return entitySnapshotDumpCommand(act.Exec), nil
	}
	act, ok := entityAction(spec, "export")
	if !ok {
		return "", fmt.Errorf("snapshots are not supported for %q", t.Service)
	}
	cmd, err := expandEntityCommand(act.Exec, t.Database)
	if err != nil {
		return "", err
	}
	return entitySnapshotDumpCommand(cmd), nil
}

// snapshotRestoreCommand builds the in-container shell command that loads a
// gzipped dump piped onto its stdin, through the declared import action.
func snapshotRestoreCommand(t SnapshotTarget) (string, error) {
	spec := EntityFor(t.Service, "databases")
	if t.AllDatabases {
		act, ok := entityAction(spec, "import_all")
		if !ok {
			return "", fmt.Errorf("service-wide snapshots are not supported for %q", t.Service)
		}
		return entitySnapshotRestoreCommand(act.Exec), nil
	}
	act, ok := entityAction(spec, "import")
	if !ok {
		return "", fmt.Errorf("snapshots are not supported for %q", t.Service)
	}
	cmd, err := expandEntityCommand(act.Exec, t.Database)
	if err != nil {
		return "", err
	}
	return entitySnapshotRestoreCommand(cmd), nil
}

// SnapshotSupported reports whether the service declares the export and import
// actions snapshots are built from, in the requested scope.
func SnapshotSupported(service string, allDatabases bool) bool {
	spec := EntityFor(service, "databases")
	suffix := ""
	if allDatabases {
		suffix = "_all"
	}
	_, exp := entityAction(spec, "export"+suffix)
	_, imp := entityAction(spec, "import"+suffix)
	return exp && imp
}

// ListSnapshots returns the stored snapshots for a service. A non-empty
// database narrows the result to that database; an empty database lists every
// database on the service. Service-wide all-databases snapshots are included
// when includeAll is set. Results are sorted newest first.
func ListSnapshots(service, database string, includeAll bool) ([]Snapshot, error) {
	var out []Snapshot

	collect := func(scopeDir string) error {
		entries, err := os.ReadDir(scopeDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			s, err := readSnapshotMeta(filepath.Join(scopeDir, e.Name()))
			if err != nil {
				continue // skip partial or unreadable snapshots
			}
			out = append(out, s)
		}
		return nil
	}

	if database != "" {
		if err := collect(snapshotScopeDir(service, database, false)); err != nil {
			return nil, err
		}
	} else {
		dbRoot := filepath.Join(config.SnapshotsDir(), service, snapshotDBScope)
		dbDirs, err := os.ReadDir(dbRoot)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, d := range dbDirs {
			if !d.IsDir() {
				continue
			}
			if err := collect(filepath.Join(dbRoot, d.Name())); err != nil {
				return nil, err
			}
		}
	}

	if includeAll {
		if err := collect(snapshotScopeDir(service, "", true)); err != nil {
			return nil, err
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out, nil
}

// DeleteSnapshot removes a stored snapshot, erroring when it does not exist so
// callers can report the miss clearly.
func DeleteSnapshot(service, database, name string, allDatabases bool) error {
	if !allDatabases {
		if err := ValidateDatabaseName(database); err != nil {
			return err
		}
	}
	clean, err := sanitizeSnapshotName(name)
	if err != nil {
		return err
	}
	dir := snapshotDir(service, database, clean, allDatabases)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("snapshot %q not found", name)
		}
		return err
	}
	return os.RemoveAll(dir)
}

// newSnapshotDir settles a snapshot's final name and makes the directory it
// goes in, holding the service lock until the caller is done with it. Shared by
// the two ways a snapshot is taken, so a snapshot of a managed database is named
// and stored exactly like a snapshot of a local one.
func newSnapshotDir(t SnapshotTarget, name string) (dir, clean string, unlock func(), err error) {
	if strings.TrimSpace(name) == "" {
		name = "snapshot-" + timestamped()
	} else if hint, reserved := reservedSnapshotName(name); reserved {
		return "", "", nil, fmt.Errorf("%q is not a valid snapshot name — %s", strings.TrimSpace(name), hint)
	} else {
		// Stamp a user-provided name with the same UTC timestamp an auto-generated
		// one carries, so repeated snapshots of one name never collide and each
		// snapshot's time can be read straight off its name.
		name = strings.TrimSpace(name) + "-" + timestamped()
	}
	clean, err = sanitizeSnapshotName(name)
	if err != nil {
		return "", "", nil, err
	}

	unlock = lockService(t.Service)
	dir = snapshotDir(t.Service, t.Database, clean, t.AllDatabases)
	if _, statErr := os.Stat(dir); statErr == nil {
		unlock()
		return "", "", nil, fmt.Errorf("snapshot %q already exists — delete it first with db:snapshot:rm", clean)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		unlock()
		return "", "", nil, fmt.Errorf("creating snapshot dir: %w", err)
	}
	return dir, clean, unlock, nil
}

// finishSnapshot records the sidecar beside a dump that has just been written.
func finishSnapshot(dir, clean string, t SnapshotTarget, ctx SnapshotMeta) (*Snapshot, error) {
	var size int64
	if fi, err := os.Stat(filepath.Join(dir, snapshotDumpFile)); err == nil {
		size = fi.Size()
	}
	database := t.Database
	if t.AllDatabases {
		database = ""
	}
	snap := Snapshot{
		Name:         clean,
		Created:      time.Now().UTC(),
		Service:      t.Service,
		Family:       t.Family,
		Database:     database,
		AllDatabases: t.AllDatabases,
		DumpFile:     snapshotDumpFile,
		Compressed:   true,
		SizeBytes:    size,
		Site:         ctx.Site,
		GitBranch:    ctx.GitBranch,
	}
	if err := writeSnapshotMeta(dir, snap); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("writing snapshot metadata: %w", err)
	}
	return &snap, nil
}

// StoreSnapshot files a dump the caller has already taken, for a database
// servlo does not host.
//
// The container path cannot reach one of those: it execs the engine's own dump
// command inside the service's container, and a managed database has no
// container on this machine. So whoever can reach it takes the dump and hands
// it here, and it lands in the same store under the same layout, because an
// operator looking for the backup taken before a bad deploy should find it in
// one place whether or not servlo runs the database it came from.
//
// plainSQL is uncompressed; it is gzipped on the way in so the stored file is
// the same shape as a local snapshot's.
func StoreSnapshot(t SnapshotTarget, name string, ctx SnapshotMeta, plainSQL io.Reader) (*Snapshot, error) {
	if !t.AllDatabases {
		if err := ValidateDatabaseName(t.Database); err != nil {
			return nil, err
		}
	}
	dir, clean, unlock, err := newSnapshotDir(t, name)
	if err != nil {
		return nil, err
	}
	defer unlock()

	if err := writeGzip(filepath.Join(dir, snapshotDumpFile), plainSQL); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("writing the dump: %w", err)
	}
	return finishSnapshot(dir, clean, t, ctx)
}

// writeGzip streams r into path, compressed. The close order matters: the gzip
// footer is written on Close, so a file closed before the writer is flushed is
// a truncated archive that reads as a corrupt one.
func writeGzip(path string, r io.Reader) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	if _, err := io.Copy(zw, r); err != nil {
		zw.Close()
		f.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// CreateSnapshot dumps the target database (or every database when
// t.AllDatabases is set) into a new named snapshot under config.SnapshotsDir().
// An empty name is auto-generated from the current UTC time.
func CreateSnapshot(t SnapshotTarget, name string, ctx SnapshotMeta, emit func(PhaseEvent)) (*Snapshot, error) {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	if !t.AllDatabases {
		if err := ValidateDatabaseName(t.Database); err != nil {
			return nil, err
		}
	}
	dumpCmd, err := snapshotDumpCommand(t)
	if err != nil {
		return nil, err
	}
	dir, clean, unlock, err := newSnapshotDir(t, name)
	if err != nil {
		return nil, err
	}
	defer unlock()

	label := t.Database
	if t.AllDatabases {
		label = "all databases"
	}
	emit(PhaseEvent{Phase: "dumping_data", Message: "dumping " + label})
	dumpPath := filepath.Join(dir, snapshotDumpFile)
	if err := dumpToHost("servlo-"+t.Service, dumpCmd, introspectEnv(), dumpPath, dumpRestoreTimeout); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("dumping %s: %w", label, err)
	}

	snap, err := finishSnapshot(dir, clean, t, ctx)
	if err != nil {
		return nil, err
	}
	emit(PhaseEvent{Phase: "done", Message: "snapshot " + clean + " created"})
	return snap, nil
}

// RestoreSnapshot loads a stored snapshot back into its database. A per-database
// restore drops and recreates the target database first so no orphan tables
// survive; an all-databases restore replays the self-cleaning dump as-is.
func RestoreSnapshot(t SnapshotTarget, name string, emit func(PhaseEvent)) (ImportReport, error) {
	if emit == nil {
		emit = func(PhaseEvent) {}
	}
	if !t.AllDatabases {
		if err := ValidateDatabaseName(t.Database); err != nil {
			return ImportReport{}, err
		}
	}
	restoreCmd, err := snapshotRestoreCommand(t)
	if err != nil {
		return ImportReport{}, err
	}
	clean, err := sanitizeSnapshotName(name)
	if err != nil {
		return ImportReport{}, err
	}

	unlock := lockService(t.Service)
	defer unlock()

	dir := snapshotDir(t.Service, t.Database, clean, t.AllDatabases)
	snap, err := readSnapshotMeta(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return ImportReport{}, fmt.Errorf("snapshot %q not found", name)
		}
		return ImportReport{}, fmt.Errorf("reading snapshot %q: %w", name, err)
	}
	dumpPath := filepath.Join(dir, snap.DumpFile)

	if !t.AllDatabases {
		emit(PhaseEvent{Phase: "dropping_database", Message: "recreating " + t.Database})
		if _, err := DropDatabase(t.Service, t.Database); err != nil {
			return ImportReport{}, fmt.Errorf("dropping %s: %w", t.Database, err)
		}
		if _, err := CreateDatabase(t.Service, t.Database); err != nil {
			return ImportReport{}, fmt.Errorf("recreating %s: %w", t.Database, err)
		}
	}

	emit(PhaseEvent{Phase: "restoring_data", Message: "restoring " + clean})
	rep, err := restoreFromHost("servlo-"+t.Service, restoreCmd, introspectEnv(), dumpPath, dumpRestoreTimeout)
	if err != nil {
		return rep, fmt.Errorf("restoring snapshot %q: %w", name, err)
	}
	// The complaints ride back on the report rather than the phase stream, since
	// every caller here has the return value and would print them twice.
	emit(PhaseEvent{Phase: "done", Message: "snapshot " + clean + " restored"})
	return rep, nil
}
