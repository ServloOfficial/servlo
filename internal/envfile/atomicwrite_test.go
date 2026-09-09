package envfile

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
	defer syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous)

	fn()
}

// fileSizeLimit and tooBigToFit are the size a file may reach during the test
// and a value comfortably past it.
const fileSizeLimit = 16 << 20

var tooBigToFit = strings.Repeat("x", 20<<20)

// The file these writers rewrite is where the site's application key and
// database password live, and os.WriteFile empties it before it writes a byte.
// A disk that fills between those two moments leaves the site holding whatever
// fit, which for an application key nobody kept a copy of means every encrypted
// column and every signed cookie is gone. The write has to fail with the old
// file still there.
func TestAWriteThatRunsOutOfDiskLeavesTheEnvFileAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "APP_KEY=base64:kPfBeKMEXhIQwiYuHhq3vXsUmFqTOvHzKQ0dJqTnGQY=\nDB_PASSWORD=A9tqMvxNbCz2\n"
	if err := os.WriteFile(path, []byte(original), SecretMode); err != nil {
		t.Fatalf("seeding the env file: %v", err)
	}

	var err error
	underAFullDisk(t, func() {
		err = ApplyUpdates(path, map[string]string{"MAIL_PASSWORD": tooBigToFit})
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading the env file back: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("the env file is now %d bytes, and the site's secrets were in %q", len(got), original)
	}
}

// Same file, same loss, written by the WordPress-shaped path: wp-config.php
// carries the database password and the authentication salts.
func TestAWriteThatRunsOutOfDiskLeavesWpConfigAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wp-config.php")
	original := "<?php\ndefine( 'DB_PASSWORD', 'A9tqMvxNbCz2' );\ndefine( 'AUTH_KEY', 'wXqTmZnKpLrVsYbGhDfJ' );\n"
	if err := os.WriteFile(path, []byte(original), SecretMode); err != nil {
		t.Fatalf("seeding wp-config.php: %v", err)
	}

	var err error
	underAFullDisk(t, func() {
		err = ApplyPhpConstUpdates(path, map[string]string{"SMTP_PASS": tooBigToFit})
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading wp-config.php back: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("wp-config.php is now %d bytes, and the site's credentials were in %q", len(got), original)
	}
}

// And by the returned-array path, which is how a framework whose config is a
// PHP array rather than a .env gets the same values written into it.
func TestAWriteThatRunsOutOfDiskLeavesThePhpArrayAsItWas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.php")
	original := "<?php\nreturn [\n    'db' => [\n        'password' => 'A9tqMvxNbCz2',\n    ],\n];\n"
	if err := os.WriteFile(path, []byte(original), SecretMode); err != nil {
		t.Fatalf("seeding the config file: %v", err)
	}

	var err error
	underAFullDisk(t, func() {
		err = ApplyPhpArrayUpdates(path, map[string]string{"mail.password": tooBigToFit})
	})
	if err == nil {
		t.Fatal("a write that could not fit reported success")
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("reading the config file back: %v", readErr)
	}
	if string(got) != original {
		t.Errorf("the config file is now %d bytes, and the site's credentials were in %q", len(got), original)
	}
}
