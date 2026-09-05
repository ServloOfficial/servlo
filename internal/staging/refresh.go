package staging

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/dbconn"
	"github.com/ServloOfficial/servlo/internal/dbdump"
	"github.com/ServloOfficial/servlo/internal/sitetpl"
)

// Copying live onto staging, and the one thing this file is really about:
// there is no function here that takes a source and a destination.
//
// Refresh takes the staging site and reads its origin out of its own block, so
// the direction is a property of the data rather than of the argument order.
// That is deliberate. Every other shape of this API has a call site where two
// site names can be swapped, and the cost of swapping them is a client's
// production data replaced by a developer's test data. There is no confirmation
// prompt that makes that acceptable, so the mistake is made unrepresentable
// instead.

// envFiles are never copied. Staging has its own database credentials, its own
// APP_URL and usually its own mail settings, and overwriting them with live's
// would point the staging site at the production database. That is the same
// accident as copying the wrong way round, arriving by a different route.
var envFiles = []string{".env", ".env.before_servlo", ".env.servlo_override"}

// Result is what a refresh moved.
type Result struct {
	Files int
	Bytes int64
	// Database is what was loaded, empty when there was none to copy.
	Database string
	// Note is anything true and worth saying that is not a failure.
	Note string
}

// What a refresh may bring across. Both by default; either alone is useful, and
// files-only is what somebody wants when the staging database has test data in
// it they are not finished with.
type Bring struct {
	Files    bool
	Database bool
}

// Everything is the ordinary refresh.
func Everything() Bring { return Bring{Files: true, Database: true} }

// Refresh copies the live site over its staging copy.
//
// It writes over what is there and leaves anything extra alone, the same way a
// deploy does. A file added on staging and never on live survives, which is
// occasionally surprising and is much better than the alternative, which is a
// refresh that deletes work somebody had not committed yet.
func Refresh(site *config.Site, opts Bring) (Result, error) {
	if !site.IsStaging() {
		return Result{}, fmt.Errorf("%s is not a staging site. Only a staging site can be refreshed from another, "+
			"and this is what stops a refresh going the wrong way", site.Name)
	}
	origin, err := config.FindSiteByRef(site.Staging.Origin)
	if err != nil {
		return Result{}, fmt.Errorf("this staging site copies from %q, which is not a site on this server", site.Staging.Origin)
	}
	if origin.Name == site.Name || origin.Path == site.Path {
		return Result{}, fmt.Errorf("this staging site names itself as its own origin, so there is nothing to copy")
	}

	var out Result
	if opts.Files {
		files, bytes, err := copyFiles(origin.Path, site.Path)
		if err != nil {
			return out, fmt.Errorf("copying the files: %w", err)
		}
		out.Files, out.Bytes = files, bytes
	}
	if opts.Database {
		loaded, note, err := copyDatabase(origin, site)
		if err != nil {
			return out, err
		}
		out.Database, out.Note = loaded, note
	}

	site.Staging.RefreshedAt = time.Now().UTC().Format(time.RFC3339)
	if err := config.AddSite(*site); err != nil {
		return out, err
	}
	return out, nil
}

// copyFiles walks the live site and writes it over the staging one.
func copyFiles(from, to string) (int, int64, error) {
	var files int
	var bytes int64

	err := filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if skip(rel, d) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Not a regular file: a socket, a fifo, or a symlink. Symlinks are
		// recreated rather than followed, because following one that points
		// outside the site would copy the rest of the server into staging.
		if info.Mode()&os.ModeSymlink != 0 {
			return copyLink(path, target)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := copyFile(path, target, info.Mode().Perm()); err != nil {
			return err
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, err
}

// skip decides what never crosses.
func skip(rel string, d fs.DirEntry) bool {
	base := filepath.Base(rel)
	for _, name := range envFiles {
		if base == name {
			return true
		}
	}
	// A backup of an env file, which the env editor writes as .env.bkp.<stamp>.
	if strings.HasPrefix(base, ".env.") && strings.Contains(base, ".bkp.") {
		return true
	}
	return false
}

func copyFile(from, to string, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck

	// Truncating rather than replacing through a rename: a rename would break
	// a hard link the application relies on, and the file being replaced is
	// one nothing is reading at this moment anyway.
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

func copyLink(from, to string) error {
	target, err := os.Readlink(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
		return err
	}
	// Replaced rather than written through, because writing through a link
	// that already exists on staging would follow it.
	_ = os.Remove(to)
	return os.Symlink(target, to)
}

// checkDatabases decides whether there is a copy to make, and refuses the one
// that would be a disaster.
//
// Servlo derives a site's database name from the site's own name, so two sites
// on one connection cannot resolve to the same database today. The guard is
// here anyway because "cannot" is a property of a function in another package,
// and the cost of it becoming false is a live database loaded into itself while
// somebody is using it.
func checkDatabases(fromDB, toDB string, sameConnection bool) (string, error) {
	switch {
	case fromDB == "":
		return "The live site has no database servlo can name, so only the files were copied.", nil
	case toDB == "":
		return "This staging site has no database servlo can name, so only the files were copied.", nil
	case fromDB == toDB && sameConnection:
		return "", fmt.Errorf("both sites resolve to the database %s on the same connection, so a refresh "+
			"would load it into itself. Give the staging site a database of its own first", toDB)
	}
	return "", nil
}

// copyDatabase dumps the live database and loads it into staging's.
//
// Piped rather than staged through a file: a production database larger than
// the droplet's memory, or its free disk, still copies.
func copyDatabase(origin, site *config.Site) (string, string, error) {
	fromDB := sitetpl.DBName(origin.Path)
	toDB := sitetpl.DBName(site.Path)
	note, err := checkDatabases(fromDB, toDB, origin.Database == site.Database)
	if err != nil {
		return "", "", err
	}
	if note != "" {
		return "", note, nil
	}

	fromConn, err := dbconn.Named(origin.Database)
	if err != nil {
		return "", "", fmt.Errorf("the live site's database connection: %w", err)
	}
	toConn, err := dbconn.Named(site.Database)
	if err != nil {
		return "", "", fmt.Errorf("this staging site's database connection: %w", err)
	}

	pr, pw := io.Pipe()
	errc := make(chan error, 1)
	go func() {
		err := dbdump.Dump(fromConn, fromDB, pw)
		// Closing with the error is what makes the client stop rather than sit
		// waiting on a stream that will never finish.
		_ = pw.CloseWithError(err)
		errc <- err
	}()
	loadErr := dbdump.Load(toConn, toDB, pr)
	if dumpErr := <-errc; dumpErr != nil {
		return "", "", fmt.Errorf("dumping %s: %w", fromDB, dumpErr)
	}
	if loadErr != nil {
		return "", "", fmt.Errorf("loading into %s: %w", toDB, loadErr)
	}
	return toDB, "", nil
}
