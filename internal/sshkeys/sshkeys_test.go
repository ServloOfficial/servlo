package sshkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Real key material, so the fingerprints are the ones ssh-keygen -l prints.
const (
	ed = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIH8kPzUFN0Nlj8Kk8Yc4LxMxbF6mYt3xI0DfKLPjXWmQ"
	rs = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDLbT8Y2vNzGkVYw8m3wRZmT1nJ1lIhbFhVQ0Xo9uYwqZ3aRr5t7vX2mB1cD4eF6gH8jK0lM2nO4pQ6rS8tU0vW2xY4zA6bC8dE0fG2hI4jK6lM8nO0pQ2rS4tU6vW8xY0zA2bC4dE6fG8hI0jK2lM4nO6pQ8rS0tU2vW4xY6zA8bC0dE2fG4hI6jK8lM0nO2pQ4rS6tU8vW0xY2zA4bC6dE8fG0hI2jK4lM6nO8pQ0rS2tU4vW6xY8zA0bC2dE4fG6hI8jK0lM2nO4pQ6rS8tU0vW2xY4z"
)

func home(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func read(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(Path())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAdd_AuthorisesAKeyAndNamesIt(t *testing.T) {
	home(t)

	key, err := Add(ed+" laptop", "")
	if err != nil {
		t.Fatal(err)
	}
	if key.Type != "ssh-ed25519" || key.Comment != "laptop" {
		t.Errorf("read the key as %+v", key)
	}
	if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Errorf("fingerprint %q is not the form ssh-keygen prints", key.Fingerprint)
	}
	if got, err := List(); err != nil || len(got) != 1 {
		t.Fatalf("listed %v, %v", got, err)
	}
}

// The file is how the operator gets in. Every write keeps every key servlo did
// not put there, or a panel eventually locks somebody out of their own server.
func TestAdd_KeepsWhatWasAlreadyThere(t *testing.T) {
	dir := home(t)
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	existing := "# added by the provider at build time\n" + rs + " provisioning\n"
	if err := os.WriteFile(Path(), []byte(existing), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Add(ed, "alex"); err != nil {
		t.Fatal(err)
	}
	body := read(t)
	if !strings.Contains(body, "provisioning") {
		t.Error("the key that was already there is gone")
	}
	if !strings.Contains(body, "# added by the provider") {
		t.Error("a comment somebody wrote in the file was removed")
	}
	if !strings.Contains(body, "alex") {
		t.Error("the new key is not there")
	}
}

// A file whose last line has no newline would otherwise have the new key
// appended to the end of it, breaking both.
func TestAdd_DoesNotJoinItselfToAFileWithNoTrailingNewline(t *testing.T) {
	dir := home(t)
	if err := os.MkdirAll(filepath.Join(dir, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(rs+" provisioning"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Add(ed, "alex"); err != nil {
		t.Fatal(err)
	}
	if got, err := List(); err != nil || len(got) != 2 {
		t.Fatalf("listed %d keys, want 2:\n%s", len(got), read(t))
	}
}

// Pasting the same key twice means somebody wants it to work. Two identical
// lines is a file that looks tampered with.
func TestAdd_TheSameKeyTwiceIsOneLine(t *testing.T) {
	home(t)
	if _, err := Add(ed, "alex"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(ed+" a-different-name", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := List(); len(got) != 1 {
		t.Errorf("the same key was authorised %d times", len(got))
	}
}

// sshd refuses to read an authorized_keys anyone but its owner can write, and
// the failure is a silent refusal of the key rather than an error anyone sees.
func TestAdd_WritesTheModesSshdInsistsOn(t *testing.T) {
	dir := home(t)
	if _, err := Add(ed, "alex"); err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := file.Mode().Perm(); perm != 0600 {
		t.Errorf("authorized_keys is %04o, which sshd will refuse to read", perm)
	}
	sshDir, err := os.Stat(filepath.Join(dir, ".ssh"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := sshDir.Mode().Perm(); perm != 0700 {
		t.Errorf(".ssh is %04o, which sshd will refuse to read", perm)
	}
}

// A private key pasted into the wrong box is worth naming, because the person
// doing it does not know they have done it.
func TestAdd_RefusesWhatIsNotAPublicKey(t *testing.T) {
	home(t)
	for _, bad := range []string{
		"",
		"   ",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA==\n-----END OPENSSH PRIVATE KEY-----",
		"not a key at all",
		"ssh-ed25519 this-is-not-base64 alex",
	} {
		if _, err := Add(bad, "alex"); err == nil {
			t.Errorf("accepted %q as a public key", firstLine(bad))
		}
	}
	if got, _ := List(); len(got) != 0 {
		t.Errorf("something was written anyway: %v", got)
	}
}

// One key at a time, so removing one later removes what was meant. A pasted
// block would also be a way to write a line servlo never validated.
func TestAdd_RefusesAWholeFileAtOnce(t *testing.T) {
	home(t)
	if _, err := Add(ed+" one\n"+rs+" two", ""); err == nil {
		t.Error("two keys went in as one")
	}
}

// A key with no name leaves a list of identical rows that nobody dares remove.
func TestAdd_RefusesAKeyWithNoName(t *testing.T) {
	home(t)
	if _, err := Add(ed, ""); err == nil {
		t.Error("a key with no name was authorised")
	}
}

// The name is rebuilt into the line rather than written through, so a comment
// carrying a newline cannot add a second line to the file.
func TestAdd_ANameCannotWriteASecondLine(t *testing.T) {
	home(t)
	if _, err := Add(ed, "alex\n"+rs+" smuggled"); err == nil {
		t.Error("a name carrying a newline was accepted")
	}
	if got, _ := List(); len(got) != 0 {
		t.Errorf("a name smuggled in a second key:\n%s", read(t))
	}
}

func TestRemove_TakesOneAwayAndLeavesTheRest(t *testing.T) {
	home(t)
	first, err := Add(ed, "alex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Add(rs, "sam"); err != nil {
		t.Fatal(err)
	}

	removed, err := Remove(first.Fingerprint)
	if err != nil || !removed {
		t.Fatalf("remove reported %v, %v", removed, err)
	}
	got, _ := List()
	if len(got) != 1 || got[0].Comment != "sam" {
		t.Errorf("after removing alex the list is %+v", got)
	}
}

// Removing a key that is not there changes nothing and says so, rather than
// reporting success for work it did not do.
func TestRemove_SaysSoWhenThereIsNothingToRemove(t *testing.T) {
	home(t)
	if _, err := Add(ed, "alex"); err != nil {
		t.Fatal(err)
	}
	before := read(t)

	removed, err := Remove("SHA256:nothing-like-this")
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("removing a key that is not there reported success")
	}
	if read(t) != before {
		t.Error("the file was rewritten for a key that was not in it")
	}
}

// An options prefix is a restriction somebody meant, and dropping it on a
// rewrite would quietly widen what a key can do.
func TestParse_KeepsAnOptionsPrefix(t *testing.T) {
	key, ok := Parse(`from="203.0.113.4",no-pty ` + ed + " deploy")
	if !ok {
		t.Fatal("a key with options was not read as a key")
	}
	if !strings.Contains(key.Options, "203.0.113.4") {
		t.Errorf("the restriction was dropped: %+v", key)
	}
	if key.Comment != "deploy" {
		t.Errorf("the name was read as %q", key.Comment)
	}
}

// A server nobody has ever added a key to has no file, which is not an error.
func TestList_NoFileIsNotAFailure(t *testing.T) {
	home(t)
	got, err := List()
	if err != nil || len(got) != 0 {
		t.Errorf("listed %v, %v", got, err)
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i] + "…"
	}
	return s
}
