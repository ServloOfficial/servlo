package atomicfile

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

const kept = "the previous contents, which nothing else on the machine holds a copy of\n"

// This package exists for one reason, written at the top of it: a disk that
// fills between os.WriteFile emptying a file and finishing the write leaves
// whatever fit and loses the rest. The registry, the machine's configuration,
// the alert list, the operator's authorized_keys and the rest all route through
// Write on the strength of that claim, and nothing here had ever made a write
// actually run out of room to see whether it holds.
func TestWrite_LeavesThePreviousContentsWhenTheWriteCannotFit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}

	var err error
	underAFullDisk(t, func() {
		err = Write(path, []byte(tooBigToFit), 0o600)
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("the file is gone: %v", readErr)
	}
	if string(got) != kept {
		t.Errorf("the previous contents did not survive: got %d bytes, want the original %d", len(got), len(kept))
	}

	// And the staging file went with the failure rather than being left to be
	// mistaken for state, or to hold the space the disk had just run out of.
	entries, readErr := os.ReadDir(filepath.Dir(path))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		if e.Name() != "state.json" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}

// The counterpart, so the harness above is not quietly proving nothing: the
// same payload through os.WriteFile really does destroy the file. If this ever
// stops failing, the test above has stopped exercising a full disk.
func TestWriteFile_DestroysTheFileWhenTheWriteCannotFit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(kept), 0o600); err != nil {
		t.Fatal(err)
	}

	underAFullDisk(t, func() {
		_ = os.WriteFile(path, []byte(tooBigToFit), 0o600)
	})

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(got) == kept {
		t.Fatal("os.WriteFile left the original intact, so this machine is not reproducing a full disk and the test above proves nothing")
	}
}
