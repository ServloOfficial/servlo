package config

import (
	"os"
	"strings"
	"testing"
)

func secretEnv(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// The password reaches a database's environment, so a weak one is a database
// anyone on the machine can open.
func TestServicePassword_IsLongAndRandom(t *testing.T) {
	secretEnv(t)
	pw, err := ServicePassword()
	if err != nil {
		t.Fatalf("ServicePassword: %v", err)
	}
	if len(pw) < 24 {
		t.Errorf("password is %d characters, too short to be worth generating", len(pw))
	}

	secretEnv(t)
	other, err := ServicePassword()
	if err != nil {
		t.Fatal(err)
	}
	if pw == other {
		t.Error("two installs generated the same password")
	}
}

// These land in connection URLs like postgresql://user:PASSWORD@host/db, where
// a colon, slash or at-sign silently produces a URL that parses into the wrong
// pieces.
func TestServicePassword_IsSafeInAConnectionURL(t *testing.T) {
	secretEnv(t)
	pw, err := ServicePassword()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{":", "/", "@", "?", "#", "%", " "} {
		if strings.Contains(pw, bad) {
			t.Errorf("password contains %q, which would break a connection URL: %s", bad, pw)
		}
	}
}

// Generated once and reused. Regenerating on every read would rewrite the
// credential out from under a database that is already running with the old one.
func TestServicePassword_IsStableAcrossReads(t *testing.T) {
	secretEnv(t)
	first, err := ServicePassword()
	if err != nil {
		t.Fatal(err)
	}
	second, err := ServicePassword()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("the password changed between reads: %q then %q", first, second)
	}
}

func TestServicePassword_FileIsOwnerOnly(t *testing.T) {
	secretEnv(t)
	if _, err := ServicePassword(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ServicePasswordFile())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("password file mode = %#o, want 0600", perm)
	}
}

// A truncated write leaves something too short to be one servlo generated.
// Trusting it would leave a database on a two-character password forever.
func TestServicePassword_ReplacesATruncatedFile(t *testing.T) {
	secretEnv(t)
	if _, err := ServicePassword(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ServicePasswordFile(), []byte("ab"), 0o600); err != nil {
		t.Fatal(err)
	}
	pw, err := ServicePassword()
	if err != nil {
		t.Fatal(err)
	}
	if len(pw) < 24 {
		t.Errorf("a truncated password file was trusted: %q", pw)
	}
}
