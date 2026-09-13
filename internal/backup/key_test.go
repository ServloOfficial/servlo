package backup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func withConfigHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return dir
}

// The key is generated once and then never changes. A second generation would
// silently orphan every backup taken before it, which is the one failure a
// backup system must not have.
func TestKey_IsCreatedOnceAndReused(t *testing.T) {
	withConfigHome(t)

	first, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != KeySize {
		t.Fatalf("key is %d bytes, want %d", len(first), KeySize)
	}
	second, err := Key()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("a second call generated a new key, orphaning every backup taken with the first")
	}
}

// It is the one secret that opens every backup this server has ever written,
// so it is not readable by anyone else on the box.
func TestKey_IsWrittenPrivate(t *testing.T) {
	dir := withConfigHome(t)
	if _, err := Key(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "servlo", keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("key file is %04o, want 0600", perm)
	}
}

// A key file that is the wrong length is a corrupted or half-written one.
// Using it would encrypt with something nobody can reproduce, so it is refused
// rather than quietly replaced: replacing it is how the old backups become
// unopenable without anyone being told.
func TestKey_RefusesAKeyOfTheWrongLength(t *testing.T) {
	dir := withConfigHome(t)
	if err := os.MkdirAll(filepath.Join(dir, "servlo"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "servlo", keyFile), []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Key(); err == nil {
		t.Error("a truncated key file was accepted")
	}
}

// The write is exercised in a child process. The file size limit that makes a
// write run out of room is process wide, and go test holds its own log file
// open throughout a run, so imposing the limit in this process fails the run
// rather than the write under test.
const tornWriteChild = "SERVLO_BACKUP_TORN_WRITE"

// A key file of the wrong length is refused for the life of the server, and
// ImportKey refuses to write over one, so between them a half written key file
// is permanent: no backup that server has taken can be opened again, and the
// rebuild that was importing the key cannot retry. That makes the write itself
// the thing that must not be able to leave a partial file, which os.WriteFile
// can, because it empties the file before it writes a byte.
func TestKey_LeavesNoHalfWrittenKeyWhenTheDiskFills(t *testing.T) {
	if mode := os.Getenv(tornWriteChild); mode != "" {
		tornWriteUnderAFullDisk(mode)
		return
	}

	for _, mode := range []string{"generate", "import"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, "servlo")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()[:strings.Index(t.Name(), "/")]+"$")
			cmd.Env = append(os.Environ(), tornWriteChild+"="+mode, "XDG_CONFIG_HOME="+home)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("the child process did not run: %v\n%s", err, out)
			}
			switch {
			case bytes.Contains(out, []byte("no file size limit:")):
				t.Skipf("this machine will not take a file size limit: %s", out)
			case !bytes.Contains(out, []byte("write failed:")):
				t.Fatalf("a key write that could not fit reported success: %s", out)
			}

			if raw, readErr := os.ReadFile(filepath.Join(dir, keyFile)); readErr == nil {
				t.Fatalf("a partial key file was left behind, and nothing will ever replace it: %d bytes", len(raw))
			} else if !errors.Is(readErr, os.ErrNotExist) {
				t.Fatal(readErr)
			}

			// And the staging file went with the failure rather than being
			// left where an operator would mistake it for the key.
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, e := range entries {
				t.Errorf("a leftover file was left behind: %s", e.Name())
			}
		})
	}
}

// tornWriteUnderAFullDisk is the child half: it caps the size of any file this
// process can write at a few bytes, which is what a droplet whose disk has
// filled does to a write, and reports what the key write did. The kernel
// raises SIGXFSZ alongside the error and its default action would take the
// process down before it could report anything, so it is ignored.
func tornWriteUnderAFullDisk(mode string) {
	signal.Ignore(syscall.SIGXFSZ)

	var previous syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous); err != nil {
		fmt.Println("no file size limit:", err)
		return
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 16, Max: previous.Max}); err != nil {
		fmt.Println("no file size limit:", err)
		return
	}

	var err error
	switch mode {
	case "generate":
		_, err = Key()
	case "import":
		err = ImportKey(strings.Repeat("ab", KeySize))
	}
	syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous) //nolint:errcheck

	if err != nil {
		fmt.Println("write failed:", err)
		return
	}
	fmt.Println("write succeeded")
}
