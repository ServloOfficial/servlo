package sftpaccess

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageKeysFile points the package at a temporary authorized_keys holding the
// operator's own key, which is the line servlo must never be able to lose.
func stageKeysFile(t *testing.T, operatorLine string) string {
	t.Helper()
	dir := t.TempDir()
	sshDir := filepath.Join(dir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(path, []byte(operatorLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := authorizedKeysPath
	authorizedKeysPath = func() (string, error) { return path, nil }
	t.Cleanup(func() { authorizedKeysPath = orig })
	return path
}

// authorized_keys is the one file servlo rewrites that servlo cannot rebuild.
// Everything outside its own block is the operator's: the key their laptop logs
// in with, the key their CI deploys with, whatever else was there before servlo
// arrived. os.WriteFile empties a file before it writes a byte, so a disk that
// fills in between leaves whatever fit and the rest is gone for good, because
// nothing anywhere else on the machine holds a copy of a public key somebody
// pasted in once.
//
// The package doc says this file only ever appends keys and never touches a
// line servlo did not write. Replacing the file by rename is what makes that
// true of a write that cannot finish as well as of one that can, so what this
// checks is that the operator ends up with a different file rather than the old
// one rewritten underneath them.
func TestWrite_ReplacesAuthorizedKeysRatherThanRewritingIt(t *testing.T) {
	const operator = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOperatorKeyForThisTest operator@laptop"
	path := stageKeysFile(t, operator)

	first := Key{Site: "shop", Label: "one", Type: "ssh-ed25519", SitePath: "/srv/shop", blob: "AAAAfirst"}
	if err := write([]Key{first}); err != nil {
		t.Fatalf("write: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	second := Key{Site: "blog", Label: "two", Type: "ssh-ed25519", SitePath: "/srv/blog", blob: "AAAAsecond"}
	if err := write([]Key{first, second}); err != nil {
		t.Fatalf("write: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("authorized_keys was rewritten in place, so a write that runs out of disk destroys the operator's own keys")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), operator) {
		t.Errorf("the operator's own key did not survive the write:\n%s", body)
	}
	if got := after.Mode().Perm(); got != 0o600 {
		t.Errorf("authorized_keys mode = %v, want 0600: sshd ignores a file anyone else can read", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "authorized_keys" {
			t.Errorf("a staging file was left in .ssh: %s", e.Name())
		}
	}
}
