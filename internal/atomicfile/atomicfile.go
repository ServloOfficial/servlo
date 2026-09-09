// Package atomicfile writes small state files the way servlo's daemons need:
// atomically, and only when the content actually changed.
package atomicfile

import (
	"bytes"
	"os"
	"path/filepath"
)

// Write replaces path with data, and never leaves it holding less than it held
// before.
//
// os.WriteFile empties a file before it writes a byte, so a disk that fills in
// between (the case internal/monitor exists to warn about) leaves whatever fit
// and loses the rest. Staging the contents beside the file and renaming means a
// write that cannot fit costs the temp file instead. The temp name is unique, so
// two writers racing for the same path do not clobber each other's staging, and
// it is created 0600 by CreateTemp and widened only at the end, so a file
// holding a credential is never briefly readable at a wider mode.
//
// The rename is done on the resolved path, so a file symlinked somewhere shared
// keeps its symlink rather than being replaced by a regular one.
func Write(path string, data []byte, perm os.FileMode) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Chmod(perm); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// WriteIfChanged is Write, skipped entirely when path already holds exactly
// these bytes.
//
// The skip is the point. A daemon that re-persists a snapshot on a fixed tick
// spends a write, a rename and a dirtied page every tick even when nothing
// happened, which on a laptop keeps the disk and the writeback path from ever
// settling. Comparing against what is already there costs one read of a small,
// page-cached file. Reports whether it wrote.
func WriteIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	return true, Write(path, data, perm)
}
