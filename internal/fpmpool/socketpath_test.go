package fpmpool

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The limit is checked against the kernel rather than taken on trust: a path at
// maxSocketPath must bind and one byte longer must not. If a platform ever
// disagrees this fails here rather than on a droplet, where the symptom is an
// FPM master that will not start.
func TestMaxSocketPath_IsWhereTheKernelActuallyStops(t *testing.T) {
	dir := t.TempDir()
	// Build the site name that lands the socket exactly on the limit.
	fixed := len(dir) + 1 + len(".sock")
	if fixed >= maxSocketPath {
		t.Skipf("the temp directory %q is already too long to test the boundary", dir)
	}
	atLimit := strings.Repeat("a", maxSocketPath-fixed)

	path := SocketPath(dir, atLimit)
	if len(path) != maxSocketPath {
		t.Fatalf("built a path of %d, wanted %d", len(path), maxSocketPath)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("a path of exactly %d bytes would not bind, so the limit is too high: %v", maxSocketPath, err)
	}
	ln.Close()      //nolint:errcheck
	os.Remove(path) //nolint:errcheck

	over := SocketPath(dir, atLimit+"a")
	if ln, err := net.Listen("unix", over); err == nil {
		ln.Close()      //nolint:errcheck
		os.Remove(over) //nolint:errcheck
		t.Errorf("a path of %d bytes bound anyway, so the limit is too low", len(over))
	}
}

// A site's pool listens on the host path, because the FPM container and
// servlo-nginx mount that directory at the same absolute place. So the length of
// the operator's home directory is part of whether a site can have a pool at
// all, and a handle the character rules are perfectly happy with can still name
// a socket nothing can bind.
//
// FPM does not skip a pool it cannot bind. It fails to start, and every site
// sharing that PHP version goes down with it, which is the outcome the bounds in
// this file exist to keep away from the master process.
func TestSocketFits_RefusesAPathTheMasterCouldNotBind(t *testing.T) {
	const dir = "/home/servlo/.local/share/servlo/run/fpm"

	short := "harborlist-example"
	if !SocketFits(dir, short) {
		t.Errorf("an ordinary site was refused a pool: %s", SocketPath(dir, short))
	}

	// Sixty-two characters: inside the sixty-four the handle rules allow, and
	// past what this socket directory leaves room for.
	long := strings.Repeat("a", 62)
	if !UsableHandle(long) {
		t.Fatal("the premise of this test is a handle the character rules accept")
	}
	if SocketFits(dir, long) {
		t.Errorf("a socket path of %d bytes was accepted", len(SocketPath(dir, long)))
	}
	if UsablePool(dir, long) {
		t.Error("UsablePool accepted a site whose socket could not be bound")
	}
}

// Write must not put an unbindable pool on disk whatever the caller checked.
func TestWrite_RefusesAPoolWhoseSocketCouldNotBeBound(t *testing.T) {
	poolDir := t.TempDir()
	socketDir := filepath.Join(t.TempDir(), strings.Repeat("d", 60))

	site := strings.Repeat("a", 60)
	if _, err := Write(poolDir, Settings{Site: site, Root: "/srv/site", SocketDir: socketDir}); err == nil {
		t.Error("a pool whose socket cannot be bound was written anyway")
	}
	if entries, err := os.ReadDir(poolDir); err == nil && len(entries) != 0 {
		t.Errorf("it left %d file(s) behind", len(entries))
	}
}
