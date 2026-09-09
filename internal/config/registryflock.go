package config

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// The registry is written by three processes, not three goroutines.
//
// siteWriteMu spans the load, the mutate and the save inside one process, which
// is what stops two panel requests losing each other. It says nothing at all
// about the servlo CLI an operator runs over SSH, or the watcher pass that
// repairs a vhost, both of which do the same sequence against the same file at
// the same time. The write is atomic, so the failure is not a corrupt
// sites.yaml: it is a setting that silently goes back to what it was, with
// nothing logged and nothing returned, which is the hardest kind to be told
// about by the person it happened to.
//
// A kernel lock is what covers that, and flock is the right one here because it
// is released when the holding process exits however it exits. A lock file with
// a pid in it would survive a kill -9 and need a staleness rule nobody would
// ever get right.
//
// Readers are deliberately not held to it. SaveSites writes a temporary file and
// renames it into place, so a reader sees the whole of one version or the whole
// of the other and never a torn file. Taking the lock to read would serialise
// every dashboard render behind every write for no gain.

// registryLockWait bounds how long a caller waits for the lock. Every hold is a
// small read and a small write, so contention is measured in microseconds and
// anything approaching this is a process that is stuck rather than busy. Failing
// with a sentence beats a panel request that never returns.
var registryLockWait = 5 * time.Second

// registryLockPoll is how often the wait retries. Short enough that an ordinary
// overlap costs nothing measurable.
const registryLockPoll = 2 * time.Millisecond

// lockRegistry takes the cross-process lock and returns the release. The lock
// lives beside sites.yaml rather than on it, so the rename SaveSites does cannot
// pull the locked file out from under a waiter.
func lockRegistry() (release func(), err error) {
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(SitesFile()+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening the registry lock: %w", err)
	}
	deadline := time.Now().Add(registryLockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK {
			_ = f.Close()
			return nil, fmt.Errorf("locking the registry: %w", err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("another servlo process has held the site registry for more than %s, so this change was not made", registryLockWait)
		}
		time.Sleep(registryLockPoll)
	}
}
