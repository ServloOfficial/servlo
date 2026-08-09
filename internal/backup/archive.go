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

	"github.com/realrashid/servlo/internal/config"
)

// What the archive holds, at fixed paths so a restore does not have to guess.
const (
	// ManifestName describes the backup: which site, when, and what is in here.
	ManifestName = "manifest.json"
	// DatabaseName is the SQL dump, absent when the site has no database.
	DatabaseName = "database.sql"
	// filesPrefix is where the site's own directory lands.
	filesPrefix = "files"
)

// FormatVersion is the archive layout. It rides in the manifest so a restore
// on a newer servlo can tell an old archive from a corrupt one.
const FormatVersion = 1

// Manifest is what an archive says about itself.
//
// It is read before anything is written to disk, which is what lets a restore
// refuse an archive for the wrong site, and what lets the panel list what is
// in a backup without unpacking it.
type Manifest struct {
	Format int `json:"format"`
	// Kind is what this archive holds: one site, or the server's own state. A
	// restore reads it before unpacking, so a server archive is never emptied
	// over a site directory.
	Kind         string    `json:"kind,omitempty"`
	Servlo       string    `json:"servlo"`
	Site         string    `json:"site"`
	Domains      []string  `json:"domains"`
	Framework    string    `json:"framework,omitempty"`
	PHPVersion   string    `json:"php_version,omitempty"`
	Taken        time.Time `json:"taken"`
	Database     bool      `json:"database"`
	DatabaseName string    `json:"database_name,omitempty"`
	Connection   string    `json:"connection,omitempty"`
	Excluded     []string  `json:"excluded,omitempty"`
	Files        int       `json:"files"`
	Bytes        int64     `json:"bytes"`
}

// ParseManifest reads a manifest and refuses one this servlo cannot make sense
// of, rather than restoring half of it.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("the archive's manifest is unreadable: %w", err)
	}
	if m.Format == 0 || m.Format > FormatVersion {
		return Manifest{}, fmt.Errorf("this backup is format %d and this servlo understands up to %d: update servlo to restore it", m.Format, FormatVersion)
	}
	return m, nil
}

// Options is one backup to take.
type Options struct {
	Site config.Site
	// Excludes are site-relative paths to leave out, which is what keeps a
	// backup from being a disk image: vendor and node_modules come back from
	// composer and npm.
	Excludes []string
	// Database streams the site's dump. Nil for a site that has none, and the
	// manifest records which it was so a restore never quietly brings back an
	// empty database.
	Database     func(io.Writer) error
	DatabaseName string
	Connection   string
	// Version is the servlo that wrote the archive, recorded for a rebuild that
	// has to know what it is looking at.
	Version string
	Taken   time.Time
}

// Create writes one encrypted backup to w and returns what it recorded.
//
// The order is deliberate: files first, then the dump, then the manifest last.
// Writing the manifest last means an archive that has one is an archive that
// finished, so an interrupted backup is recognisable as incomplete rather than
// discovered to be short at restore time.
func Create(w io.Writer, key []byte, opts Options) (Manifest, error) {
	if opts.Taken.IsZero() {
		opts.Taken = time.Now().UTC()
	}
	man := Manifest{
		Format:       FormatVersion,
		Kind:         KindSite,
		Servlo:       opts.Version,
		Site:         opts.Site.Name,
		Domains:      opts.Site.Domains,
		Framework:    opts.Site.Framework,
		PHPVersion:   opts.Site.PHPVersion,
		Taken:        opts.Taken.UTC(),
		DatabaseName: opts.DatabaseName,
		Connection:   opts.Connection,
		Excluded:     opts.Excludes,
	}

	enc, err := NewEncryptor(w, key)
	if err != nil {
		return Manifest{}, err
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)

	files, bytes, err := writeTree(tw, opts.Site.Path, opts.Excludes)
	if err != nil {
		return Manifest{}, err
	}
	man.Files, man.Bytes = files, bytes

	if opts.Database != nil {
		n, err := writeDump(tw, opts.Database)
		if err != nil {
			return Manifest{}, err
		}
		man.Database = true
		man.Bytes += n
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

// writeTree walks the site into the archive under files/.
//
// Only regular files and directories go in. A symlink is deliberately not
// followed: copying what it points at would pull the rest of the server into
// one site's backup, and writing it back out on restore would land outside the
// site. Devices, sockets and fifos are skipped for the same reason there is no
// sense in restoring them.
func writeTree(tw *tar.Writer, root string, excludes []string) (int, int64, error) {
	skip := excludeSet(excludes)
	var files int
	var total int64

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
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
		if excluded(rel, skip) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		name := path.Join(filesPrefix, rel)
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeDir, Name: name + "/",
				Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(),
			})
		case info.Mode().IsRegular():
			if err := tw.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg, Name: name, Size: info.Size(),
				Mode: int64(info.Mode().Perm()), ModTime: info.ModTime(),
			}); err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			n, copyErr := io.Copy(tw, f)
			_ = f.Close()
			if copyErr != nil {
				return copyErr
			}
			files++
			total += n
			return nil
		default:
			return nil
		}
	})
	if err != nil {
		return 0, 0, fmt.Errorf("reading %s: %w", root, err)
	}
	return files, total, nil
}

// writeDump streams the database into the archive.
//
// tar wants a size up front and a dump's is not known until it has run, so it
// is spooled to a temporary file first. Holding a multi-gigabyte dump in memory
// on a droplet that is also serving the sites is not an option.
func writeDump(tw *tar.Writer, dump func(io.Writer) error) (int64, error) {
	spool, err := os.CreateTemp("", "servlo-backup-*.sql")
	if err != nil {
		return 0, err
	}
	defer os.Remove(spool.Name()) //nolint:errcheck
	defer spool.Close()           //nolint:errcheck

	if err := dump(spool); err != nil {
		return 0, fmt.Errorf("dumping the database: %w", err)
	}
	size, err := spool.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	if _, err := spool.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: DatabaseName, Size: size, Mode: 0600,
	}); err != nil {
		return 0, err
	}
	return size, copyExactly(tw, spool, size)
}

// copyExactly guards the one way a spooled dump goes wrong quietly: tar was
// promised a byte count, and a short copy leaves an archive whose header and
// contents disagree.
func copyExactly(w io.Writer, r io.Reader, size int64) error {
	n, err := io.Copy(w, r)
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("the dump changed size while it was being written: %d bytes of %d", n, size)
	}
	return nil
}

func writeFile(tw *tar.Writer, name string, body []byte, mode int64, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg, Name: name, Size: int64(len(body)), Mode: mode, ModTime: mod,
	}); err != nil {
		return err
	}
	_, err := tw.Write(body)
	return err
}

func excludeSet(excludes []string) map[string]bool {
	set := make(map[string]bool, len(excludes))
	for _, e := range excludes {
		e = strings.Trim(filepath.ToSlash(strings.TrimSpace(e)), "/")
		if e != "" && e != "." {
			set[e] = true
		}
	}
	return set
}

// excluded reports whether this entry is named by the exclude list.
//
// Matching the entry itself is enough to drop its whole tree: the walk answers
// SkipDir for an excluded directory, so nothing under it is ever visited to be
// asked about.
func excluded(rel string, skip map[string]bool) bool { return skip[rel] }

// readTarFile pulls one named file out of a tar stream.
//
// The manifest is written last, so finding it means reading past everything
// else. That is the cost of the ordering that makes an unfinished archive
// recognisable, and it is paid only when something asks what a backup holds.
func readTarFile(r io.Reader, name string) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("this archive has no %s: it was interrupted before it finished", name)
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if h.Name == name {
			return io.ReadAll(tr)
		}
	}
}
