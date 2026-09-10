package config

import (
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// underAFullDisk runs fn with the process limited to files of fileSizeLimit
// bytes, so a write of more than that fails partway through exactly as it does
// on a droplet whose disk has filled. The kernel also raises SIGXFSZ, whose
// default action would take the test binary down with it, so it is ignored for
// the duration. The limit is generous, and the payload correspondingly large,
// because go test has its own log file open throughout the run and a limit
// small enough to catch a realistic payload would fail that too.
func underAFullDisk(t *testing.T, fn func()) {
	t.Helper()
	signal.Ignore(syscall.SIGXFSZ)
	defer signal.Reset(syscall.SIGXFSZ)

	var previous syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous); err != nil {
		t.Skipf("this machine will not report its file size limit: %v", err)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: fileSizeLimit, Max: previous.Max}); err != nil {
		t.Skipf("this machine will not take a file size limit: %v", err)
	}
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous) //nolint:errcheck

	fn()
}

const fileSizeLimit = 16 << 20

var tooBigToFit = strings.Repeat("x", 20<<20)

// writeFileAtomic is the writer behind sites.yaml and config.yaml, which are
// between them every site on the machine and how it is set up. A truncated
// config.yaml does not even fail to parse: it reads as defaults, so the parked
// directory silently becomes the built-in one and the panel's domain becomes
// none.
//
// The tests beside it prove the file is replaced rather than rewritten, by
// comparing inodes. This proves what that is for, by actually running the write
// out of room.
func TestWriteFileAtomic_LeavesThePreviousContentsWhenTheWriteCannotFit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sites.yaml")
	const kept = "sites:\n  - name: harborlist\n    path: /srv/harborlist\n"
	if err := os.WriteFile(path, []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}

	var err error
	underAFullDisk(t, func() {
		err = writeFileAtomic(path, []byte(tooBigToFit), 0o600)
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the registry is gone: %v", readErr)
	}
	if string(got) != kept {
		t.Errorf("the registry did not survive: got %d bytes, want the original %d", len(got), len(kept))
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		if e.Name() != "sites.yaml" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}
