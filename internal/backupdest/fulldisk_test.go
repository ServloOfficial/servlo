package backupdest

import (
	"os/signal"
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
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous)

	fn()
}

// fileSizeLimit and tooBigToFit are the size a file may reach during the test
// and a value comfortably past it.
const fileSizeLimit = 16 << 20

var tooBigToFit = strings.Repeat("x", 20<<20)

// The destinations file holds the S3 secret key and the SFTP credentials that
// are the only route archives take off this machine. A write truncated by a
// full disk leaves YAML that no longer parses, which stops every later backup
// from being copied anywhere and takes the credentials with it.
func TestADestinationThatCannotBeWrittenLeavesTheOthersReadable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	existing := Destination{Name: "offsite", Kind: KindSFTP, Host: "backup.example.com", User: "servlo", Path: "/srv/archives"}
	if err := save(Registry{Destinations: []Destination{existing}}); err != nil {
		t.Fatalf("seeding the destinations: %v", err)
	}

	var err error
	underAFullDisk(t, func() {
		err = save(Registry{Destinations: []Destination{existing, {Name: "second", Kind: KindS3, Bucket: "archives", Prefix: tooBigToFit}}})
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	reg, loadErr := Load()
	if loadErr != nil {
		t.Fatalf("the destinations no longer load: %v", loadErr)
	}
	if len(reg.Destinations) != 1 || reg.Destinations[0].Host != existing.Host {
		t.Errorf("destinations are now %d entries, and the offsite copy was going to %s", len(reg.Destinations), existing.Host)
	}
}
