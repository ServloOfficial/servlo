package deploykey

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ".pub" is a real TLD, so "blog.example" and "blog.example.pub" are both
// domains somebody can own.
//
// While the key files were <site> and <site>.pub in one flat directory, those
// two names collided: the second site's *private* key was written over the
// first site's *public* key file. The first site then read its private key back
// as its public one, and the panel displayed it for the operator to paste into
// GitHub. A key layout that can do that is the wrong layout, whatever the
// probability of the domain.
func TestEnsure_ASiteCannotOverwriteAnothersKey(t *testing.T) {
	isolate(t)

	plain, err := Ensure("blog.example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure("blog.example.pub"); err != nil {
		t.Fatal(err)
	}

	again, err := Ensure("blog.example")
	if err != nil {
		t.Fatal(err)
	}
	if again.Public != plain.Public {
		t.Error("the second site changed the first site's key")
	}
	if strings.Contains(again.Public, "PRIVATE KEY") {
		t.Fatal("the public key field carries a private key")
	}
	if !strings.HasPrefix(again.Public, "ssh-ed25519 ") {
		t.Errorf("public key = %.40q, not a public key line", again.Public)
	}
}

// Every file under the key directory is a credential or names one, so the
// directory is 0700 and each private key 0600 no matter how they are laid out.
func TestEnsure_EveryPrivateKeyIs0600(t *testing.T) {
	isolate(t)

	for _, site := range []string{"a.example", "b.example", "a.example.pub"} {
		key, err := Ensure(site)
		if err != nil {
			t.Fatalf("Ensure(%q): %v", site, err)
		}
		info, err := os.Stat(key.PrivatePath)
		if err != nil {
			t.Fatalf("stat %q: %v", key.PrivatePath, err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s: mode = %o, want 600", site, perm)
		}
	}

	root, err := os.Stat(Dir())
	if err != nil {
		t.Fatal(err)
	}
	if perm := root.Mode().Perm(); perm != 0o700 {
		t.Errorf("key directory mode = %o, want 700", perm)
	}
}

// Removing one site's key must not remove another's, which a flat layout also
// got wrong: removing "blog.example" deleted "blog.example.pub".
func TestRemove_LeavesTheOtherSiteAlone(t *testing.T) {
	isolate(t)

	if _, err := Ensure("blog.example"); err != nil {
		t.Fatal(err)
	}
	neighbour, err := Ensure("blog.example.pub")
	if err != nil {
		t.Fatal(err)
	}

	if err := Remove("blog.example"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(neighbour.PrivatePath); err != nil {
		t.Errorf("removing one site took another site's key: %v", err)
	}
}

// A site's key lives under the key directory and nowhere else, whatever the
// domain spells.
func TestEnsure_KeepsKeysUnderTheKeyDirectory(t *testing.T) {
	isolate(t)

	key, err := Ensure("blog.example")
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(Dir(), key.PrivatePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("private key at %q is outside %q", key.PrivatePath, Dir())
	}
}
