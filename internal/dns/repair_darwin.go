//go:build darwin

package dns

import "os"

// RepairPossible reports whether the DNS watcher can rewrite
// /etc/resolver/<tld> from the current process. macOS requires root to
// touch /etc/resolver, so the watcher relies on the passwordless sudo
// drop-in installed by `servlo install` (renderDarwinSudoers). Without that
// file in place every repair attempt would prompt for a password and fail
// non-interactively, spamming the log.
//
// We detect "drop-in is installed" by stat'ing /etc/sudoers.d/servlo. The
// drop-in's content is the contract; if a user manually trimmed or moved
// it, the gate stays open and repairs will still be attempted (visudo /
// sudoers parsers will surface the issue more usefully than this stat).
func RepairPossible() bool {
	_, err := os.Stat("/etc/sudoers.d/servlo")
	return err == nil
}
