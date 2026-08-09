package sitefs

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Extracting an archive into a directory that already has things in it.
//
// This is not siteops.Unzip, and the difference is deliberate. That one unpacks
// a whole new site into an empty directory and strips a single wrapping
// directory on the way, because a "Download ZIP" from a git host wraps
// everything in one. This one drops an archive into a directory that already
// holds a site, which is what a plugin or a theme is, and stripping the wrapper
// there would install the plugin's contents where the plugin should be.
//
// The refusal is common to both: SafeEntryName lives here, in the leaf package,
// and siteops calls it, so zip slip has one spelling in the codebase.
//
// The second half of the jail is per entry rather than per archive. A name that
// looks harmless still has to resolve inside the site root, because the
// destination the operator chose might itself be a symlink pointing somewhere
// else entirely.

// UnzipLimits bounds what an archive may become.
type UnzipLimits struct {
	MaxBytes int64
	MaxFiles int
}

// UnzipResult is what came out.
type UnzipResult struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

// Unzip extracts an archive into dirRel, overwriting files that are already
// there and leaving everything else alone.
func (r Root) Unzip(dirRel string, ra io.ReaderAt, size int64, limits UnzipLimits) (UnzipResult, error) {
	var res UnzipResult

	dir, err := r.ResolveExisting(dirRel)
	if err != nil {
		return res, err
	}
	if info, err := os.Stat(dir); err != nil {
		return res, err
	} else if !info.IsDir() {
		return res, fmt.Errorf("%s is not a directory", displayName(dirRel))
	}
	base := r.Rel(dir)

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return res, fmt.Errorf("that file is not a zip archive servlo can read: %w", err)
	}
	if len(zr.File) > limits.MaxFiles {
		return res, fmt.Errorf("the archive holds %d files, more than the %d servlo will extract", len(zr.File), limits.MaxFiles)
	}

	// Every entry is checked, and every destination resolved, before a single
	// byte is written. An archive that is refused halfway is an archive that
	// has already planted whatever came before the entry that was caught.
	targets, err := r.planEntries(zr, base, limits)
	if err != nil {
		return res, err
	}

	for _, f := range zr.File {
		name := entryName(f)
		target := targets[name]
		if target == "" {
			continue
		}
		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(target, dirMode); err != nil {
				return res, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return res, err
		}
		written, err := writeEntry(f, target, limits.MaxBytes)
		if err != nil {
			return res, err
		}
		res.Bytes += written
		res.Files++
	}
	return res, nil
}

// planEntries validates every entry and resolves where each one lands.
func (r Root) planEntries(zr *zip.Reader, base string, limits UnzipLimits) (map[string]string, error) {
	targets := make(map[string]string, len(zr.File))
	var declared int64

	for _, f := range zr.File {
		name := entryName(f)
		if err := SafeEntryName(name); err != nil {
			return nil, err
		}
		// A symlink is a second way out of the directory, and no archive an
		// operator drops into a live site needs one badly enough to be worth
		// checking its target.
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("the archive contains a symlink (%q), which servlo does not extract", name)
		}
		if !f.Mode().IsRegular() && !f.Mode().IsDir() {
			return nil, fmt.Errorf("the archive contains %q, which is neither a file nor a directory", name)
		}
		declared += int64(f.UncompressedSize64)
		if declared > limits.MaxBytes {
			return nil, fmt.Errorf("the archive is too large: it expands to more than %s", humanBytes(limits.MaxBytes))
		}

		trimmed := strings.TrimSuffix(name, "/")
		if trimmed == "" {
			continue
		}
		// The jail, per entry. The name having survived SafeEntryName says
		// nothing about the destination, which may be a symlink out of the site
		// that the operator picked from the listing.
		target, err := r.ResolveNew(path.Join(base, trimmed))
		if err != nil {
			return nil, fmt.Errorf("the archive entry %q would land outside the site directory", name)
		}
		targets[name] = target
	}
	return targets, nil
}

// writeEntry copies one entry, refusing to write more than limit bytes.
//
// Written with O_TRUNC rather than O_EXCL, because dropping a plugin update
// over the plugin is the case this exists for. The mode comes from servlo, not
// from the archive: an execute bit in a zip is a decision somebody else made
// about a file on this machine.
func writeEntry(f *zip.File, target string, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("the archive is too large")
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close() //nolint:errcheck

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, modeFor(f.Name, fileMode))
	if err != nil {
		return 0, err
	}
	defer out.Close() //nolint:errcheck

	written, err := io.Copy(out, io.LimitReader(rc, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, fmt.Errorf("the archive is too large: it expands past the limit servlo will extract")
	}
	return written, nil
}

// entryName is the archive's own spelling of an entry, with the "./" some
// writers prepend removed.
func entryName(f *zip.File) string {
	return strings.TrimPrefix(f.Name, "./")
}

// SafeEntryName refuses any archive entry name that would not land inside the
// directory it is being extracted into.
//
// The rules are about the name a zip writer put in the archive rather than
// about any particular destination, so they hold wherever the entry is going,
// which is why both extraction paths in servlo call this one function.
func SafeEntryName(name string) error {
	if name == "" {
		return fmt.Errorf("the archive contains an entry with no name")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("the archive contains an entry whose name has a NUL byte")
	}
	// Backslashes are a path separator on the machine that wrote the archive,
	// so `..\x` is a traversal that a forward-slash check alone reads as a
	// perfectly ordinary filename.
	if strings.Contains(name, `\`) {
		return fmt.Errorf("the archive entry %q contains a backslash", name)
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
		return fmt.Errorf("the archive entry %q is an absolute path", name)
	}
	// path.Clean resolves the traversal; anything still climbing after that is
	// climbing out.
	cleaned := path.Clean(name)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return fmt.Errorf("the archive entry %q points outside the site directory", name)
	}
	return nil
}
