package siteops

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/realrashid/servlo/internal/sitefs"
)

// Extracting an uploaded archive.
//
// This is the one place in servlo where a file the operator did not write
// decides a path servlo writes to, so it is written as a refusal engine with
// extraction as a side effect rather than the other way round.
//
// Three things it will not do. It will not write outside the directory it was
// given, which is zip slip and the only vulnerability this code has ever been
// famous for. It will not extract more than it was told to, because a small
// archive that expands to fill the disk takes every other site on the machine
// down with it. And it will not honour a mode from the archive, because every
// site here runs as the same user and an execute bit in a zip is a decision
// somebody else made about a file on this machine.
//
// Extraction goes into a scratch directory beside the target and is moved into
// place only once the whole archive has been read, so a refusal leaves nothing
// behind for the operator to clear up before retrying.

// UnzipOptions bounds what an archive is allowed to become.
type UnzipOptions struct {
	// MaxBytes is the most the archive may expand to, uncompressed.
	MaxBytes int64
	// MaxFiles is the most entries it may contain.
	MaxFiles int
	// StripPrefix names the single top-level directory to remove, for a caller
	// that knows what the archive is. Empty means infer it: strip the only
	// top-level directory when there is exactly one, and otherwise strip
	// nothing.
	//
	// The difference matters for a pinned release. Inference is right for an
	// operator's upload, where servlo has no idea what is in the archive. For a
	// definition that names the wrapper, a release that stops shipping one
	// should fail loudly rather than quietly install a directory deeper than
	// the vhost expects.
	StripPrefix string
}

// UnzipResult describes what was extracted.
type UnzipResult struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	// Unwrapped records that the archive held everything inside one directory
	// and that directory was removed on the way in.
	Unwrapped bool `json:"unwrapped"`
}

// fileMode and dirMode are what everything extracted gets, regardless of what
// the archive asked for. PHP is read by the FPM pool, not executed by the
// kernel, so nothing in a site needs an execute bit to be served.
const (
	fileMode = 0o644
	dirMode  = 0o755
)

// Unzip extracts an archive into dir, which must be empty.
func Unzip(r io.ReaderAt, size int64, dir string, opts UnzipOptions) (UnzipResult, error) {
	var res UnzipResult

	if entries, err := os.ReadDir(dir); err != nil {
		return res, fmt.Errorf("reading %q: %w", dir, err)
	} else if len(entries) > 0 {
		return res, fmt.Errorf("the directory is not empty: extract into an empty directory, or add the existing project as a folder instead")
	}

	zr, err := zip.NewReader(r, size)
	if err != nil {
		return res, fmt.Errorf("that file is not a zip archive servlo can read: %w", err)
	}
	if len(zr.File) > opts.MaxFiles {
		return res, fmt.Errorf("the archive holds %d files, more than the %d servlo will extract", len(zr.File), opts.MaxFiles)
	}

	// Every entry is checked before any of them is written, so an archive that
	// is refused halfway is refused before it has touched anything.
	prefix, err := planEntries(zr, opts)
	if err != nil {
		return res, err
	}

	// A scratch directory beside the target rather than inside it: same
	// filesystem, so the move at the end is a rename, and a refusal or a crash
	// leaves the target as empty as it was found.
	scratch, err := os.MkdirTemp(filepath.Dir(dir), ".servlo-unzip-")
	if err != nil {
		return res, err
	}
	defer os.RemoveAll(scratch)

	for _, f := range zr.File {
		name := strings.TrimPrefix(entryName(f), prefix)
		if name == "" {
			continue
		}
		target := filepath.Join(scratch, filepath.FromSlash(name))

		if strings.HasSuffix(entryName(f), "/") {
			if err := os.MkdirAll(target, dirMode); err != nil {
				return res, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
			return res, err
		}
		written, err := writeEntry(f, target, opts.MaxBytes)
		if err != nil {
			return res, err
		}
		res.Bytes += written
		res.Files++
	}

	if err := moveContents(scratch, dir); err != nil {
		return res, err
	}
	res.Unwrapped = prefix != ""
	return res, nil
}

// planEntries validates every entry and returns the single top-level directory
// to strip, empty when there is not exactly one.
//
// Validating the whole archive first is what makes the refusal clean: an entry
// that escapes is found before anything has been written, not halfway through.
func planEntries(zr *zip.Reader, opts UnzipOptions) (string, error) {
	want := strings.TrimSuffix(opts.StripPrefix, "/")
	var declared int64
	tops := map[string]bool{}
	sawFileAtRoot := false

	for _, f := range zr.File {
		name := entryName(f)
		if err := safeEntryName(name); err != nil {
			return "", err
		}
		// A symlink pointing anywhere is a second way out of the directory, and
		// no site needs one badly enough to be worth checking its target.
		if f.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("the archive contains a symlink (%q), which servlo does not extract", name)
		}
		if !f.Mode().IsRegular() && !f.Mode().IsDir() {
			return "", fmt.Errorf("the archive contains %q, which is neither a file nor a directory", name)
		}

		// The declared size is a claim, and writeEntry enforces the real one.
		// Checking it here refuses an honest zip bomb before extracting a byte.
		declared += int64(f.UncompressedSize64)
		if declared > opts.MaxBytes {
			return "", fmt.Errorf("the archive is too large: it expands to more than %d bytes", opts.MaxBytes)
		}

		trimmed := strings.TrimSuffix(name, "/")
		if trimmed == "" {
			continue
		}
		if first, _, nested := strings.Cut(trimmed, "/"); nested {
			tops[first] = true
		} else {
			tops[trimmed] = true
			if !strings.HasSuffix(name, "/") {
				sawFileAtRoot = true
			}
		}
	}

	// A caller that named the wrapper gets it checked rather than inferred.
	if want != "" {
		if !tops[want] || sawFileAtRoot {
			return "", fmt.Errorf("the archive does not hold everything inside %q, so servlo will not strip it", want)
		}
		return want + "/", nil
	}

	// "Download ZIP" wraps everything in one directory named for the repository
	// or the release. Extracting that verbatim gives a site whose document root
	// is one level below where anybody would look for it.
	if len(tops) == 1 && !sawFileAtRoot {
		for only := range tops {
			return only + "/", nil
		}
	}
	return "", nil
}

// safeEntryName refuses any name that would not land inside the directory.
//
// The rules live in internal/sitefs, the leaf package the file manager's own
// extraction also goes through. Zip slip with two spellings in one codebase is
// zip slip with one of them out of date.
func safeEntryName(name string) error {
	return sitefs.SafeEntryName(name)
}

// writeEntry copies one entry, refusing to write more than limit bytes.
//
// The archive's total is not this function's business: planEntries owns that,
// and refuses an oversized archive before a byte is written rather than after
// most of it is on disk. What this owns is one entry against the ceiling, so a
// stream that delivers more than its header claimed becomes an error instead
// of the LimitReader's silent truncation. Today it does not fire, because
// archive/zip validates the stream against the declared size itself and
// refuses the entry first; keeping it costs three lines and does not depend on
// that staying true.
func writeEntry(f *zip.File, target string, limit int64) (int64, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("the archive is too large")
	}
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	// limit+1 so hitting the limit exactly is distinguishable from exceeding
	// it, rather than a file of exactly the limit being refused.
	written, err := io.Copy(out, io.LimitReader(rc, limit+1))
	if err != nil {
		return written, err
	}
	if written > limit {
		return written, fmt.Errorf("the archive is too large: it expands past the limit servlo will extract")
	}
	return written, nil
}

// moveContents moves everything from src into dst, which is empty.
func moveContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// entryName is the archive's own spelling of an entry, with the "./" some
// writers prepend removed so the top-level check sees the real names.
func entryName(f *zip.File) string {
	return strings.TrimPrefix(f.Name, "./")
}
