package push

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
)

// withTempDataDir points config.DataDir at a fresh tmpdir for the test and
// restores XDG_DATA_HOME on cleanup. The cached VAPID keys are reset so the
// next VAPIDKeys() call re-reads from the new dir.
func withTempDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev, hadPrev := os.LookupEnv("XDG_DATA_HOME")
	t.Setenv("XDG_DATA_HOME", dir)
	resetForTest()
	t.Cleanup(func() {
		if hadPrev {
			os.Setenv("XDG_DATA_HOME", prev)
		} else {
			os.Unsetenv("XDG_DATA_HOME")
		}
		resetForTest()
	})
	return dir
}

func TestVAPIDKeys_GeneratesAndPersists(t *testing.T) {
	withTempDataDir(t)

	priv, pub, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}
	if priv == "" || pub == "" {
		t.Fatalf("empty key pair: priv=%q pub=%q", priv, pub)
	}
	// Public key is base64url-encoded raw P-256 (65 bytes uncompressed),
	// which encodes to 87 chars without padding.
	if got := len(pub); got < 80 || got > 90 {
		t.Errorf("public key len = %d, want ~87", got)
	}
	if strings.Contains(pub, "+") || strings.Contains(pub, "/") {
		t.Errorf("public key contains non-url-safe chars: %s", pub)
	}

	priv2, pub2, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys (second call): %v", err)
	}
	if priv2 != priv || pub2 != pub {
		t.Errorf("second call returned different keys: priv=%v pub=%v", priv2 != priv, pub2 != pub)
	}
}

func TestVAPIDKeys_ReloadsFromDisk(t *testing.T) {
	withTempDataDir(t)

	priv, pub, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}

	// Clear the in-memory cache so the next call has to read the persisted
	// files. The pair must match what was generated.
	resetForTest()
	priv2, pub2, err := VAPIDKeys()
	if err != nil {
		t.Fatalf("VAPIDKeys (post-reset): %v", err)
	}
	if priv2 != priv || pub2 != pub {
		t.Errorf("disk reload returned different keys")
	}
}

func TestVAPIDKeys_PrivateKeyFilePermissions(t *testing.T) {
	dir := withTempDataDir(t)
	if _, _, err := VAPIDKeys(); err != nil {
		t.Fatalf("VAPIDKeys: %v", err)
	}
	info, err := os.Stat(dir + "/servlo/" + vapidPrivateFile)
	if err != nil {
		t.Fatalf("stat private key: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("private key perm = %o, want 0600", mode)
	}
}

// The write is exercised in a child process. The file size limit that makes a
// write run out of room is process wide, and go test holds its own log file
// open throughout a run, so imposing the limit in this process fails the run
// rather than the write under test.
const tornWriteChild = "SERVLO_PUSH_TORN_WRITE"

// The reader accepts any non-empty pair, so a write torn by a full disk leaves
// a private key too short to sign with next to whichever public key was
// already on disk, and pushes fail from then on without saying why. The write
// has to be all or nothing.
func TestVAPIDKeys_LeaveNoHalfWrittenKeyWhenTheDiskFills(t *testing.T) {
	if os.Getenv(tornWriteChild) != "" {
		tornWriteUnderAFullDisk()
		return
	}

	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^"+t.Name()+"$")
	cmd.Env = append(os.Environ(), tornWriteChild+"=1", "XDG_DATA_HOME="+home)
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

	t.Setenv("XDG_DATA_HOME", home)
	resetForTest()
	t.Cleanup(resetForTest)
	if priv, _, ok := readKeyPairFromDisk(); ok {
		t.Fatalf("a partial key pair was left behind and read back as usable: the private key is %d bytes", len(priv))
	}
	if raw, readErr := os.ReadFile(privPath()); readErr == nil {
		t.Fatalf("a partial private key file was left behind: %d bytes", len(raw))
	}
}

// tornWriteUnderAFullDisk is the child half: it caps the size of any file this
// process can write at a few bytes, which is what a droplet whose disk has
// filled does to a write, and reports what generating the pair did. The kernel
// raises SIGXFSZ alongside the error and its default action would take the
// process down before it could report anything, so it is ignored.
func tornWriteUnderAFullDisk() {
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

	_, _, err := VAPIDKeys()
	syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous) //nolint:errcheck

	if err != nil {
		fmt.Println("write failed:", err)
		return
	}
	fmt.Println("write succeeded")
}
