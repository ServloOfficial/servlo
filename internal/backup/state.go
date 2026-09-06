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
	"time"

	"github.com/ServloOfficial/servlo/internal/config"
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

	// Only the registry from the data directory. The rest of it is caches,
	// certificates that will be reissued, and the archives themselves, none of
	// which belongs inside a backup of the thing that made them.
	if n, size, err := writeStateFile(tw, sitesFilePath(), path.Join(dataPrefix, "sites.yaml")); err != nil {
		return Manifest{}, err
	} else {
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
// what lists them: they sit in the same directory as the sites' own.
const StateName = "servlo-state"

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
