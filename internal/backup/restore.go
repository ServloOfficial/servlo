package backup

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// RestoreFiles writes the site's files out of an archive into dir, and returns
// what the archive said about itself.
//
// An archive is a file from somewhere else. It may have come off object storage
// nobody here controls, or off a machine that is being rebuilt precisely because
// something went wrong with it. So every entry is checked against the directory
// it is being written into rather than trusted, and anything that is not a
// plain file or a directory is skipped entirely.
func RestoreFiles(r io.Reader, key []byte, dir string) (Manifest, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return Manifest{}, err
	}

	var man Manifest
	var seenManifest bool
	err = walkArchive(r, key, func(h *tar.Header, body io.Reader) error {
		if h.Name == ManifestName {
			raw, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			man, err = ParseManifest(raw)
			if err != nil {
				return err
			}
			if man.Kind == KindState {
				return errors.New("this is a backup of the server's own state, not of a site: restore it with servlo restore --state")
			}
			seenManifest = true
			return nil
		}
		if h.Name == DatabaseName {
			// Restored by OpenDump, on its own pass, into the engine rather
			// than onto disk.
			return nil
		}
		if prefix, _, _ := strings.Cut(h.Name, "/"); prefix == configPrefix || prefix == dataPrefix {
			// The manifest is written last, so the archive announces itself by
			// its contents before it announces itself by name. Saying which
			// command this is beats a generic complaint about an odd entry.
			return errors.New("this is a backup of the server's own state, not of a site: restore it with servlo restore --state")
		}
		rel, ok := strings.CutPrefix(h.Name, filesPrefix+"/")
		if !ok {
			// Everything a servlo archive holds is the manifest, the dump, or
			// something under files/. An entry outside all three is either a
			// malformed archive or a hostile one, and skipping it quietly would
			// restore part of something that should not be restored at all.
			//
			// This is also why adding a new top-level entry to the archive is a
			// format change: an older servlo has to refuse it rather than half
			// unpack it.
			return fmt.Errorf("this archive has an entry servlo does not recognise (%s), so it is not restored", h.Name)
		}
		target, err := contain(root, rel)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(target, permOr(h.Mode, 0755))
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, permOr(h.Mode, 0644))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, body)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		default:
			// A symlink is a way to write through it on the next entry, and a
			// device or socket is not something a site's backup should carry.
			// Skipped rather than checked, because there is no version of
			// restoring one that is worth the risk.
			return nil
		}
	})
	if err != nil {
		return Manifest{}, err
	}
	if !seenManifest {
		return Manifest{}, errors.New("this archive has no manifest: it was interrupted before it finished")
	}
	return man, nil
}

// OpenDump streams the archive's database dump to w and returns the manifest.
//
// Streamed rather than returned, because a dump can be larger than the droplet's
// memory and the caller is about to pipe it straight into an engine.
func OpenDump(r io.Reader, key []byte, w io.Writer) (Manifest, error) {
	var man Manifest
	var found bool
	err := walkArchive(r, key, func(h *tar.Header, body io.Reader) error {
		switch h.Name {
		case ManifestName:
			raw, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			man, err = ParseManifest(raw)
			return err
		case DatabaseName:
			found = true
			_, err := io.Copy(w, body)
			return err
		}
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	if !found {
		return man, errors.New("this archive holds no database dump")
	}
	return man, nil
}

// walkArchive opens an archive and hands each entry to fn in order.
//
// The manifest is written last, so the format version is not known until the
// end. Rather than restore first and check afterwards, entries are read twice
// where it matters: RestoreFiles reads the manifest through this same walk and
// ParseManifest refuses an unreadable one, which stops the walk before the rest
// of the archive is written.
func walkArchive(r io.Reader, key []byte, fn func(*tar.Header, io.Reader) error) error {
	dec, err := NewDecryptor(r, key)
	if err != nil {
		return err
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return fmt.Errorf("reading the archive: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading the archive: %w", err)
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// contain resolves an archive entry against the directory it is going into and
// refuses anything that lands outside.
//
// The check is on the cleaned, absolute result rather than on the text of the
// name, because "a/../../b" and "./../b" and a name with a leading slash all
// escape and none of them is caught by looking for "..".
func contain(root, rel string) (string, error) {
	if rel == "" {
		return "", errors.New("this archive has an entry with no name")
	}
	target := filepath.Clean(filepath.Join(root, rel))
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("this archive tries to write %s, which is outside the site: refusing to restore it", rel)
	}
	return target, nil
}

// permOr turns a tar mode into a file mode, falling back when an archive carries
// nothing usable. A zero mode would create a file nobody can read.
func permOr(mode int64, fallback os.FileMode) os.FileMode {
	perm := os.FileMode(mode).Perm()
	if perm == 0 {
		return fallback
	}
	return perm
}

// RestoreState writes a server-state archive back into the config and data
// directories.
//
// It refuses a site archive, and RestoreFiles refuses this one, because the two
// unpack to completely different places and getting them the wrong way round
// would empty a server's configuration over a site directory or the reverse.
func RestoreState(r io.Reader, key []byte, configDir, dataDir string) (Manifest, error) {
	roots := map[string]string{configPrefix: configDir, dataPrefix: dataDir}
	for _, dir := range roots {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return Manifest{}, err
		}
	}

	var man Manifest
	var seen bool
	err := walkArchive(r, key, func(h *tar.Header, body io.Reader) error {
		if h.Name == ManifestName {
			raw, err := io.ReadAll(body)
			if err != nil {
				return err
			}
			man, err = ParseManifest(raw)
			if err != nil {
				return err
			}
			if man.Kind != KindState {
				return fmt.Errorf("this is a backup of the site %s, not of the server: restore it with servlo restore", man.Site)
			}
			seen = true
			return nil
		}
		prefix, rel, ok := strings.Cut(h.Name, "/")
		if prefix == filesPrefix || h.Name == DatabaseName {
			return errors.New("this is a backup of a site, not of the server: restore it with servlo restore")
		}
		root, known := roots[prefix]
		if !ok || !known {
			return fmt.Errorf("this archive has an entry servlo does not recognise (%s), so it is not restored", h.Name)
		}
		target, err := contain(root, rel)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			return os.MkdirAll(target, permOr(h.Mode, 0700))
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				return err
			}
			// The modes travel with the files, because half of what is in here
			// is a credential and 0600 is not decoration.
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, permOr(h.Mode, 0600))
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, body)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			return closeErr
		default:
			return nil
		}
	})
	if err != nil {
		return Manifest{}, err
	}
	if !seen {
		return Manifest{}, errors.New("this archive has no manifest: it was interrupted before it finished")
	}
	return man, nil
}
