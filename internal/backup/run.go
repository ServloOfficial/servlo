package backup

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/realrashid/servlo/internal/config"
)

// Extension is what a servlo backup is called on disk. It is not .tar.gz,
// because it is not one: unzipping it without the key gets nowhere, and a name
// that suggests otherwise wastes an operator's afternoon.
const Extension = ".servlobak"

// Record is one archive that was written.
type Record struct {
	Path     string
	Size     int64
	Manifest Manifest
	// Pruned is how many older archives the retention sweep removed, and
	// PruneError why it stopped. A sweep that failed leaves the backup itself
	// untouched, so the two are reported rather than raised.
	Pruned     int
	PruneError error
	// SendErrors is one entry per destination that could not be reached. The
	// archive is on disk regardless, so these are reported beside a successful
	// backup rather than instead of one.
	SendErrors []error
}

// Runner takes a backup of a site.
//
// The two seams are the ones that need the rest of the tree: which paths to
// leave out, and how to dump this site's database. Keeping them as functions is
// what lets this package stay under siteops and serviceops rather than
// alongside them, and what lets every test above run without a database.
type Runner struct {
	// Dir is where finished archives land.
	Dir string
	// Excludes resolves the site's exclude list.
	Excludes func(*config.Site) ([]string, error)
	// Dump returns a function that streams the site's dump, plus the database
	// and connection it came from. A nil function means the site has no
	// database, which is recorded rather than treated as a failure.
	Dump func(*config.Site) (func(io.Writer) error, string, string)
	// Version is the servlo that took the backup, stamped into the manifest.
	Version string
	// Policy is how much history this site keeps. The sweep runs as part of
	// the backup rather than as a second timer, because a retention timer
	// nobody armed is a disk that fills and takes the sites down with it.
	Policy func(*config.Site) Policy
	// Send copies the finished archive somewhere that is not this server. A
	// backup that only exists on the machine it is a backup of is not one, and
	// this is the step that fixes that.
	Send func(path, name string) []error
	// Now is the clock, so a test can name the file it expects.
	Now func() time.Time
}

// Run writes one backup and returns what landed.
//
// It is written under a temporary name in the destination directory and moved
// into place only once it is complete. A half-written archive that kept its
// final name would be counted by the next retention sweep and picked up by a
// restore, which is a worse outcome than no backup at all.
func (r Runner) Run(site *config.Site) (Record, error) {
	if site == nil {
		return Record{}, fmt.Errorf("no site to back up")
	}
	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	key, err := Key()
	if err != nil {
		return Record{}, err
	}

	opts := Options{Site: *site, Version: r.Version, Taken: now}
	if r.Excludes != nil {
		excludes, err := r.Excludes(site)
		if err != nil {
			return Record{}, fmt.Errorf("resolving what to leave out: %w", err)
		}
		opts.Excludes = excludes
	}
	if r.Dump != nil {
		opts.Database, opts.DatabaseName, opts.Connection = r.Dump(site)
	}

	if err := os.MkdirAll(r.Dir, 0700); err != nil {
		return Record{}, err
	}
	tmp, err := os.CreateTemp(r.Dir, ".partial-*")
	if err != nil {
		return Record{}, err
	}
	tmpName := tmp.Name()
	// Every path out of here that is not a completed rename removes the
	// partial. Remove after a successful rename is a no-op on a name that no
	// longer exists, so one deferred cleanup covers both.
	defer os.Remove(tmpName) //nolint:errcheck

	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return Record{}, err
	}
	man, err := Create(tmp, key, opts)
	if err != nil {
		_ = tmp.Close()
		return Record{}, err
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		_ = tmp.Close()
		return Record{}, err
	}
	if err := tmp.Close(); err != nil {
		return Record{}, err
	}

	final, err := uniquePath(r.Dir, site.Name, now)
	if err != nil {
		return Record{}, err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return Record{}, err
	}

	rec := Record{Path: final, Size: size, Manifest: man}
	if r.Send != nil {
		// Failures here are reported, not raised. The archive is on this server
		// and usable; what failed is the copy going elsewhere, and calling the
		// whole backup failed would have an operator re-running one that worked.
		rec.SendErrors = r.Send(final, filepath.Base(final))
	}
	if r.Policy != nil {
		// A sweep that fails is a warning, not a failed backup. The archive is
		// already on disk and reporting otherwise would have an operator
		// re-running something that worked.
		removed, err := Prune(r.Dir, config.SiteSlug(site.Name), r.Policy(site), now)
		rec.Pruned = removed
		if err != nil {
			rec.PruneError = err
		}
	}
	return rec, nil
}

// uniquePath names the archive after the site and the moment, and finds a free
// one rather than overwriting. Two backups a second apart would otherwise share
// a name, and the second silently replacing the first is the loss that
// retention exists to prevent, arriving by a different route.
func uniquePath(dir, site string, now time.Time) (string, error) {
	base := fmt.Sprintf("%s-%s", config.SiteSlug(site), now.Format(stampLayout))
	for n := 0; n < 1000; n++ {
		name := base
		if n > 0 {
			name = fmt.Sprintf("%s-%d", base, n+1)
		}
		path := filepath.Join(dir, name+Extension)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("could not find a free name for a backup of %s in %s", site, dir)
}

// ReadManifest opens an archive far enough to say what it is, without unpacking
// it. A restore reads this first, so it can refuse an archive for the wrong
// site before it has written anything.
func ReadManifest(r io.Reader, key []byte) (Manifest, error) {
	dec, err := NewDecryptor(r, key)
	if err != nil {
		return Manifest{}, err
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading the archive: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	raw, err := readTarFile(gz, ManifestName)
	if err != nil {
		return Manifest{}, err
	}
	return ParseManifest(raw)
}
