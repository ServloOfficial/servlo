package backup

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
	"github.com/ServloOfficial/servlo/internal/version"
)

// What an archive is of. A site archive holds one site; a state archive holds
// what the server itself knows, which is the difference between restoring a
// site onto a working server and rebuilding the server first.
const (
	KindSite  = "site"
	KindState = "state"
)

// Where the server's own state lands inside a state archive.
const (
	configPrefix = "config"
	dataPrefix   = "data"
)

// StateOptions is one server-state backup.
type StateOptions struct {
	Version string
	Taken   time.Time
}

// CreateState writes the server's own state: everything servlo knows that is
// not a site's files or data.
//
// This is what makes a rebuild possible. Restoring a site's archive onto a
// fresh droplet gives you the files and the database and a server with no idea
// what a site is, no connections, no schedules and no settings. That is all
// here.
//
// The backup key is deliberately not in it. It is what opens this archive, so
// including it would be locking the door and taping the key to the front. The
// operator keeps it separately, which is why servlo says so every time it is
// mentioned.
func CreateState(w io.Writer, key []byte, opts StateOptions) (Manifest, error) {
	if opts.Taken.IsZero() {
		opts.Taken = time.Now().UTC()
	}
	man := Manifest{
		Format: FormatVersion,
		Kind:   KindState,
		Servlo: opts.Version,
		Taken:  opts.Taken.UTC(),
	}

	enc, err := NewEncryptor(w, key)
	if err != nil {
		return Manifest{}, err
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)

	files, bytes, err := writeStateTree(tw, config.ConfigDir(), configPrefix, map[string]bool{keyFile: true})
	if err != nil {
		return Manifest{}, err
	}
	man.Files, man.Bytes = files, bytes

	// From the data directory: the registry, and the files the operator wrote
	// by hand. The rest of it is caches, certificates that will be reissued,
	// generated config that comes back from the registry, and the archives
	// themselves, none of which belongs inside a backup of the thing that made
	// them.
	if n, size, err := writeStateFile(tw, sitesFilePath(), path.Join(dataPrefix, "sites.yaml")); err != nil {
		return Manifest{}, err
	} else {
		man.Files += n
		man.Bytes += size
	}

	for _, tree := range operatorTrees {
		n, size, err := writeStateTreeMatching(tw, filepath.Join(config.DataDir(), filepath.FromSlash(tree.rel)),
			path.Join(dataPrefix, tree.rel), tree.keep)
		if err != nil {
			return Manifest{}, err
		}
		man.Files += n
		man.Bytes += size
	}

	raw, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := writeFile(tw, ManifestName, raw, 0600, opts.Taken); err != nil {
		return Manifest{}, err
	}
	for _, closer := range []func() error{tw.Close, gz.Close, enc.Close} {
		if err := closer(); err != nil {
			return Manifest{}, err
		}
	}
	return man, nil
}

// sitesFilePath is the registry, as its own function so a test can find it
// under a temporary data home.
func sitesFilePath() string { return config.SitesFile() }

// operatorTrees are the places under the data directory that hold what the
// operator typed rather than what servlo generated.
//
// Each of these carries an explicit never-clobber contract in its own doc
// comment in internal/config/paths.go: servlo seeds the file once and never
// writes it again, so edits survive a vhost regeneration, a service reinstall
// and an update. What none of them survived was a rebuild. The archive took the
// config directory and sites.yaml, on the reasoning that everything else under
// the data directory was a cache, a certificate that would be reissued or an
// archive. That is true of everything else and false of these four, and the
// difference is that nothing else knows what was in them.
//
// The filters matter as much as the paths. A generated vhost restored onto a
// fresh machine names a certificate it has not been issued, the .bkp
// directories are copies of the files beside them, and the .aux.conf tuning
// helper is rewritten on every start.
var operatorTrees = []struct {
	rel  string
	keep func(rel string) bool
}{
	// Per-site and global nginx snippets, included at the end of a server
	// block and at http{} level respectively.
	{"nginx/custom.d", hasExt(".conf")},
	{"nginx/http.d", hasExt(".conf")},
	// Per-service runtime tuning, minus the helper servlo regenerates.
	{"service-tuning", func(rel string) bool {
		return strings.HasSuffix(rel, ".conf") && !strings.HasSuffix(rel, ".aux.conf")
	}},
	// PHP settings: per version, shared across versions, and per site. The
	// ini.bkp directories beside them are the editor's own backups.
	{"php", func(rel string) bool {
		base := path.Base(rel)
		return (base == "98-user.ini" || base == "95-shared.ini") && !strings.Contains(rel, "ini.bkp/")
	}},
}

// hasExt keeps the files with one extension and nothing else.
func hasExt(ext string) func(string) bool {
	return func(rel string) bool { return strings.HasSuffix(rel, ext) }
}

// writeStateTreeMatching copies the files under root that keep accepts, named
// relative to root. A directory that is not there yields nothing, because a
// server with no custom nginx is the ordinary case rather than an error.
func writeStateTreeMatching(tw *tar.Writer, root, prefix string, keep func(rel string) bool) (int, int64, error) {
	var files int
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return infoErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if !keep(rel) {
			return nil
		}
		n, size, err := writeStateFileFrom(tw, p, path.Join(prefix, rel), info)
		files += n
		total += size
		return err
	})
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", root, err)
	}
	return files, total, nil
}

// writeStateTree copies a directory into the archive under prefix, skipping
// anything named in skip.
func writeStateTree(tw *tar.Writer, root, prefix string, skip map[string]bool) (int, int64, error) {
	var files int
	var total int64
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skip[rel] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		name := path.Join(prefix, rel)
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir, Name: name + "/",
				Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(),
			})
		case info.Mode().IsRegular():
			n, size, err := writeStateFileFrom(tw, p, name, info)
			files += n
			total += size
			return err
		default:
			// A socket or a symlink in the config directory is runtime state,
			// not configuration, and nothing about it is worth restoring.
			return nil
		}
	})
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", root, err)
	}
	return files, total, nil
}

func writeStateFile(tw *tar.Writer, from, name string) (int, int64, error) {
	info, err := os.Stat(from)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	return writeStateFileFrom(tw, from, name, info)
}

func writeStateFileFrom(tw *tar.Writer, from, name string, info os.FileInfo) (int, int64, error) {
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Size: info.Size(),
		Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(),
	}); err != nil {
		return 0, 0, err
	}
	f, err := os.Open(from)
	if err != nil {
		return 0, 0, err
	}
	n, copyErr := io.Copy(tw, f)
	_ = f.Close()
	if copyErr != nil {
		return 0, 0, copyErr
	}
	return 1, n, nil
}

// StateName is the prefix every server-state archive carries, which is also
// what lists them: they sit in the same directory as the sites' own. It comes
// from config because that is where the registry refuses a site of the same
// name, and the two have to agree for that refusal to mean anything.
const StateName = config.StateArchiveName

// WriteState creates a server-state archive in dir and returns where it landed.
//
// The file is written under a temporary name and renamed into place, so a
// process killed halfway through leaves a .partial rather than something that
// looks like an archive and is not one. Restoring from a truncated backup is
// the failure this exists to prevent, so it must not be able to produce one.
//
// Shared by the command and the panel. Two copies of this would be two chances
// for one of them to skip the rename.
func WriteState(dir string, key []byte, opts StateOptions) (path string, man Manifest, size int64, err error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", Manifest{}, 0, err
	}
	tmp, err := os.CreateTemp(dir, ".partial-*")
	if err != nil {
		return "", Manifest{}, 0, err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck

	// The mode is set before anything is written. It holds every credential
	// servlo has, and a file that is briefly world-readable is world-readable.
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return "", Manifest{}, 0, err
	}
	man, err = CreateState(tmp, key, opts)
	if err != nil {
		_ = tmp.Close()
		return "", Manifest{}, 0, err
	}
	size, _ = tmp.Seek(0, io.SeekCurrent)
	if err := tmp.Close(); err != nil {
		return "", Manifest{}, 0, err
	}

	path = filepath.Join(dir, StateName+"-"+man.Taken.Format(stampLayout)+Extension)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", Manifest{}, 0, err
	}
	return path, man, size, nil
}

// StatePolicy is how much of the server's own history to keep.
//
// The same shape as a site's, and for the same reason: the newest archive is
// the one a rebuild uses, and the older ones are there for the day the newest
// turns out to have been taken after whatever went wrong. Keeping every one of
// them is how a disk fills quietly.
var StatePolicy = Policy{Daily: 7, Weekly: 4, Monthly: 3}

// StateRunner takes a backup of the server's own state, copies it to every
// destination and thins the older ones.
//
// It exists because none of that was happening. WriteState wrote an archive to
// local disk and stopped, so the one thing that makes a site archive
// restorable, the registry and every setting beside it, was the one thing with
// no offsite copy and no retention. The droplet dies, the site archives are
// safe somewhere else, and what comes back is a server with no idea what a
// site is.
//
// The two fields are seams, the same ones Runner has, so a test can take a
// backup without a bucket and without deleting anything it did not create. A
// nil Send skips the copy and a zero Policy skips the sweep.
type StateRunner struct {
	Dir     string
	Send    func(path, name string) []error
	Policy  Policy
	Version string
}

// ForState is the runner the command and the panel both use, so there is one
// place that decides where a state archive goes rather than two.
func ForState() StateRunner {
	return StateRunner{
		Dir:     config.SiteBackupsDir(),
		Send:    SendEverywhere,
		Policy:  StatePolicy,
		Version: version.Version,
	}
}

// Run writes the archive, sends it, and prunes what the policy no longer keeps.
//
// A destination failing is reported rather than raised, exactly as it is for a
// site: the archive is on this server and usable, and calling the whole backup
// failed would have an operator re-running one that worked. A failed sweep is
// reported the same way and for the same reason.
func (r StateRunner) Run(key []byte, opts StateOptions) (Record, error) {
	if opts.Version == "" {
		opts.Version = r.Version
	}
	if opts.Taken.IsZero() {
		opts.Taken = time.Now().UTC()
	}

	path, man, size, err := WriteState(r.Dir, key, opts)
	if err != nil {
		return Record{}, err
	}

	rec := Record{Path: path, Size: size, Manifest: man}
	if r.Send != nil {
		rec.SendErrors = r.Send(path, filepath.Base(path))
	}
	if !r.Policy.Empty() {
		// StateName rather than a site slug. The two share a directory and
		// each sweep matches its own prefix followed by a timestamp, so
		// neither can reach the other's archives.
		removed, pruneErr := Prune(r.Dir, StateName, r.Policy, opts.Taken)
		rec.Pruned = removed
		rec.PruneError = pruneErr
	}
	return rec, nil
}
