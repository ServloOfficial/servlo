package sftpaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A real ed25519 public key, so the parser has something valid to accept.
const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1ZQ9K1nQyR9m5D3VvJ8xO1XkS8pR3wHqM2vN0aB4cD alice@laptop"
const otherKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJ2aR0L2oRzS0n6E4WwK9yP2YlT9qS4xIrN3wO1bC5dE bob@desk"

func withKeyFile(t *testing.T, existing string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "authorized_keys")
	if existing != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prev := authorizedKeysPath
	authorizedKeysPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { authorizedKeysPath = prev })
	return path
}

// Additive is the whole rule. An operator's own keys, and anything another tool
// put in the file, come back out untouched.
func TestAddKeepsEveryLineServloDidNotWrite(t *testing.T) {
	foreign := "# my own keys\nssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7 me@elsewhere\n"
	path := withKeyFile(t, foreign)

	if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey); err != nil {
		t.Fatal(err)
	}
	after := read(t, path)
	if !strings.HasPrefix(after, foreign) {
		t.Fatalf("the operator's own lines were not preserved:\n%s", after)
	}
	if !strings.Contains(after, beginMarker) || !strings.Contains(after, endMarker) {
		t.Fatalf("servlo's block is not marked:\n%s", after)
	}
}

func TestRemoveTakesOnlyServlosOwnLine(t *testing.T) {
	foreign := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7 me@elsewhere\n"
	path := withKeyFile(t, foreign)
	added, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Add("beta.com", "/srv/sites/beta", "bob-desk", otherKey); err != nil {
		t.Fatal(err)
	}

	removed, err := Remove(added.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("Remove reported nothing removed")
	}
	after := read(t, path)
	if !strings.Contains(after, foreign) {
		t.Fatal("removing a servlo key removed somebody else's")
	}
	keys, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Site != "beta.com" {
		t.Fatalf("keys = %+v, want only beta.com", keys)
	}
}

func TestListReportsSiteLabelAndFingerprint(t *testing.T) {
	withKeyFile(t, "")
	added, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey)
	if err != nil {
		t.Fatal(err)
	}

	keys, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 {
		t.Fatalf("keys = %+v", keys)
	}
	got := keys[0]
	if got.Site != "acme.com" || got.Label != "alice-laptop" || got.Type != "ssh-ed25519" {
		t.Fatalf("key = %+v", got)
	}
	if !strings.HasPrefix(got.Fingerprint, "SHA256:") || got.Fingerprint != added.Fingerprint {
		t.Fatalf("fingerprint = %q", got.Fingerprint)
	}
}

// The line servlo writes has to keep the session off a shell and inside the
// site. Both live in the authorized_keys options, so both are asserted here.
func TestTheWrittenLineRestrictsTheSessionAndNamesTheSite(t *testing.T) {
	path := withKeyFile(t, "")
	if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey); err != nil {
		t.Fatal(err)
	}

	line := servloLine(t, read(t, path))
	if !strings.HasPrefix(line, "restrict,") {
		t.Fatalf("line does not start restricted: %q", line)
	}
	if !strings.Contains(line, `command="internal-sftp -d /srv/sites/acme"`) {
		t.Fatalf("line does not confine the session to the site: %q", line)
	}
	if !strings.Contains(line, "servlo-sftp site=acme.com label=alice-laptop") {
		t.Fatalf("line does not say what it is for: %q", line)
	}
}

// A label goes into the file verbatim, so a label with a newline in it would be
// a second authorized_keys line that servlo did not write and did not restrict.
func TestAddRefusesALabelThatWouldWriteASecondLine(t *testing.T) {
	path := withKeyFile(t, "")

	for _, label := range []string{
		"alice\nssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB1ZQ9K1nQyR9m5D3VvJ8xO1XkS8pR3wHqM2vN0aB4cD attacker",
		"alice\rbob",
		`alice" command="/bin/sh`,
		"alice with spaces",
		"",
		strings.Repeat("a", 200),
	} {
		if _, err := Add("acme.com", "/srv/sites/acme", label, testKey); err == nil {
			t.Fatalf("Add with label %q should be refused", label)
		}
	}
	if _, err := os.Stat(path); err == nil {
		if strings.Contains(read(t, path), "attacker") {
			t.Fatal("an injected line was written")
		}
	}
}

// The site path is interpolated into a double-quoted option, so a path with a
// quote in it would close the option and start another one.
func TestAddRefusesASitePathThatWouldBreakOutOfTheOption(t *testing.T) {
	withKeyFile(t, "")

	for _, sitePath := range []string{
		`/srv/sites/a" command="/bin/sh`,
		"/srv/sites/a\nb",
		`/srv/sites/a\b`,
		"relative/path",
		"",
	} {
		if _, err := Add("acme.com", sitePath, "alice-laptop", testKey); err == nil {
			t.Fatalf("Add with site path %q should be refused", sitePath)
		}
	}
}

func TestAddRefusesSomethingThatIsNotAPublicKey(t *testing.T) {
	withKeyFile(t, "")

	for _, key := range []string{
		"", "not a key at all",
		"ssh-ed25519 AAAAnotbase64!!",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		// An options prefix is the caller trying to write their own line.
		`command="/bin/sh" ` + testKey,
		// Two keys in one paste: only the first would ever be written, so the
		// second is a key the operator thinks they authorised and did not.
		testKey + "\n" + otherKey,
	} {
		if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", key); err == nil {
			t.Fatalf("Add with key %q should be refused", key)
		}
	}
}

func TestAddRefusesTheSameKeyTwiceForTheSameSite(t *testing.T) {
	withKeyFile(t, "")
	if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey); err != nil {
		t.Fatal(err)
	}

	if _, err := Add("acme.com", "/srv/sites/acme", "alice-again", testKey); err == nil {
		t.Fatal("the same key was authorised twice")
	}
}

func TestTheFileAndItsDirectoryAreOwnerOnly(t *testing.T) {
	path := withKeyFile(t, "")
	if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey); err != nil {
		t.Fatal(err)
	}

	// sshd refuses a group-writable authorized_keys, and an operator debugging
	// why their key stopped working will not think to check the mode.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("authorized_keys mode = %v, want 0600", info.Mode().Perm())
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if dir.Mode().Perm() != 0o700 {
		t.Fatalf(".ssh mode = %v, want 0700", dir.Mode().Perm())
	}
}

func TestForSiteFiltersToOneSite(t *testing.T) {
	withKeyFile(t, "")
	if _, err := Add("acme.com", "/srv/sites/acme", "alice-laptop", testKey); err != nil {
		t.Fatal(err)
	}
	if _, err := Add("beta.com", "/srv/sites/beta", "bob-desk", otherKey); err != nil {
		t.Fatal(err)
	}

	keys, err := ForSite("beta.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0].Label != "bob-desk" {
		t.Fatalf("keys = %+v", keys)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func servloLine(t *testing.T, content string) string {
	t.Helper()
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "restrict,") {
			return line
		}
	}
	t.Fatalf("no servlo key line in:\n%s", content)
	return ""
}
