package config

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// The lock has to hold against another process, which is the case the in-process
// mutex cannot cover and the one that actually happens: an operator running
// servlo over SSH while the panel saves a setting, or the watcher's repair pass
// landing in the same moment.
//
// flock associates the lock with the open file description rather than with the
// process, so two separate opens conflict even inside one test binary. That is
// what makes this testable without spawning anything, and it is also why the
// release closes the file rather than only unlocking it.
func TestUpdateSites_WaitsForAnotherProcessHoldingTheRegistry(t *testing.T) {
	isolateRegistry(t)

	held, err := os.OpenFile(SitesFile()+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	var done atomic.Bool
	go func() {
		UpdateSites(func(reg *SiteRegistry) (bool, error) { //nolint:errcheck
			reg.Sites = append(reg.Sites, Site{Name: "acme", Path: "/tmp/acme"})
			return true, nil
		})
		done.Store(true)
	}()

	time.Sleep(60 * time.Millisecond)
	if done.Load() {
		t.Fatal("a write went through while another process held the registry lock, so the two can lose each other")
	}

	_ = syscall.Flock(int(held.Fd()), syscall.LOCK_UN)
	_ = held.Close()

	deadline := time.Now().Add(2 * time.Second)
	for !done.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the write never completed after the other holder released")
		}
		time.Sleep(5 * time.Millisecond)
	}

	reg, err := LoadSites()
	if err != nil {
		t.Fatal(err)
	}
	if len(reg.Sites) != 1 || reg.Sites[0].Name != "acme" {
		t.Errorf("registry = %+v, want the one site the waiting write added", reg.Sites)
	}
}

// A holder that dies still releases: the kernel drops the lock when the last
// descriptor for it closes, which is what a pid file in the data directory
// would not do.
func TestRegistryLock_IsReleasedWhenTheHolderGoesAway(t *testing.T) {
	isolateRegistry(t)

	release, err := lockRegistry()
	if err != nil {
		t.Fatal(err)
	}
	release()

	second, err := lockRegistry()
	if err != nil {
		t.Fatalf("the lock was not free after its holder released: %v", err)
	}
	second()
}

// The wait is bounded, so a stuck process costs a sentence rather than a panel
// request that never returns.
func TestRegistryLock_GivesUpRatherThanHangingForever(t *testing.T) {
	isolateRegistry(t)

	held, err := os.OpenFile(SitesFile()+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(held.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	defer held.Close() //nolint:errcheck

	orig := registryLockWait
	registryLockWait = 40 * time.Millisecond
	defer func() { registryLockWait = orig }()

	start := time.Now()
	if _, err := lockRegistry(); err == nil {
		t.Fatal("the lock was taken while another holder had it")
	} else if took := time.Since(start); took > time.Second {
		t.Errorf("waited %s before giving up", took)
	}
}

// isolateRegistry points the config paths at a directory of this test's own, so
// nothing here reads or writes the machine's real registry.
func isolateRegistry(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(DataDir(), 0755); err != nil {
		t.Fatal(err)
	}
	invalidateSitesCache()
	t.Cleanup(invalidateSitesCache)
}
