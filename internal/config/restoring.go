package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// A rebuild is a window where the registry is ahead of the disk.
//
// Putting a server back is two steps in servlo's own documented order: restore
// the server's state, which brings back the site registry, and then restore
// each site, which brings back its directory and its database. Between them
// every site is registered and none of its files are here yet.
//
// The watcher's stale sweep reads that as an operator having deleted their
// projects. It drops the vhost, stops the workers and removes the registry
// entry, and the next `servlo restore` of a site archive is told the archive is
// of a site that is not on this server, which is true by then and was not a
// minute earlier. CI lost that race once in thirty seconds; an operator working
// through a dozen site archives by hand has a far wider window than that.
//
// So a restore says it is happening, and the sweep holds off while it is.

// restoreGrace is how long a restore keeps the sweep off. Generous on purpose:
// what it costs is a directory the operator really did delete lingering in the
// registry until the window closes, and what it buys is a rebuild that does not
// eat itself. Every restore pushes the window out again, so a long sequence of
// site archives stays covered.
const restoreGrace = 2 * time.Hour

func restoringMarkerPath() string { return filepath.Join(DataDir(), "restoring") }

// MarkRestoring records that a restore is under way, so the watcher's stale
// sweep leaves a registry that is ahead of the disk alone.
func MarkRestoring() error {
	if err := os.MkdirAll(DataDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(restoringMarkerPath(), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
}

// RestoreInProgress reports whether a restore has happened recently enough that
// a site with no directory is expected rather than stale.
func RestoreInProgress() bool {
	info, err := os.Stat(restoringMarkerPath())
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < restoreGrace
}

// RestoreWindowRemaining is what the watcher says when it holds off, so an
// operator reading the log knows why nothing was swept and when it will be.
func RestoreWindowRemaining() string {
	info, err := os.Stat(restoringMarkerPath())
	if err != nil {
		return ""
	}
	left := restoreGrace - time.Since(info.ModTime())
	if left <= 0 {
		return ""
	}
	return fmt.Sprintf("%s", left.Round(time.Minute))
}

// ExpireRestoreWindowForTest backdates the marker past the grace period, so a
// test can exercise what happens once a rebuild is over without waiting hours.
func ExpireRestoreWindowForTest(t interface{ Fatal(...any) }) {
	old := time.Now().Add(-restoreGrace - time.Minute)
	if err := os.Chtimes(restoringMarkerPath(), old, old); err != nil {
		t.Fatal(err)
	}
}
