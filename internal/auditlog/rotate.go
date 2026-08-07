package auditlog

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Rotation, and why it is a rename.
//
// A log nothing ever removes fills the disk. A log the application truncates is
// a log that tells you what the last writer wanted you to believe. Rotation is
// how to have both: the live file is only ever appended to, and when it grows
// past the threshold it is renamed rather than emptied. Nothing that was
// written is unwritten; it is somewhere else.
//
// Not logrotate. That would put the guarantee in a config file servlo does not
// own, on a schedule servlo cannot see, and an operator who disabled it would
// get a full disk rather than a rotation. Doing it at the append is a check on
// a size that is already in hand.

const (
	// maxLogBytes is where rotation happens. Small enough that a single file
	// stays readable in an editor and a support paste is not a megabyte, large
	// enough that an ordinary week does not roll.
	maxLogBytes = 4 << 20

	// keptRotations bounds the history. Rotation that never prunes trades one
	// unbounded file for an unbounded number of them.
	keptRotations = 5
)

// rotateIfLarger renames the live log when it exceeds limit, and prunes the
// oldest rotations. An absent log is not an error: it is the ordinary state of
// a machine where nothing has happened yet.
func rotateIfLarger(limit int64) error {
	info, err := os.Stat(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Size() <= limit {
		return nil
	}

	// Named by the moment it was rotated, so the ordering an operator reads is
	// the ordering the filenames sort in.
	target := fmt.Sprintf("%s.%s", Path(), time.Now().UTC().Format("20060102T150405Z"))
	// A rotation landing in the same second as a previous one would otherwise
	// overwrite it, which is the one way rotation could lose a line.
	for n := 1; ; n++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = fmt.Sprintf("%s.%s-%d", Path(), time.Now().UTC().Format("20060102T150405Z"), n)
	}
	if err := os.Rename(Path(), target); err != nil {
		return fmt.Errorf("rotating the audit log: %w", err)
	}
	return pruneRotations()
}

// pruneRotations keeps the newest keptRotations files and removes the rest.
func pruneRotations() error {
	matches, err := filepath.Glob(Path() + ".*")
	if err != nil {
		return err
	}
	if len(matches) <= keptRotations {
		return nil
	}
	// The names carry a sortable timestamp, so lexical order is chronological.
	sort.Strings(matches)
	for _, path := range matches[:len(matches)-keptRotations] {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// rotationFiles returns the rotated logs, newest last, for a read that spans
// them.
func rotationFiles() []string {
	matches, err := filepath.Glob(Path() + ".*")
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}
