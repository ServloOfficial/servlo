package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// handMade builds an archive entry by entry, so a test can put something in one
// that Create would never write. Every path-safety test below needs that: the
// threat is an archive from somewhere else, not one servlo made.
func handMade(t *testing.T, key []byte, entries map[string]string, man *Manifest) []byte {
	t.Helper()
	var sealed bytes.Buffer
	enc, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(body)), Mode: 0644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if man == nil {
		man = &Manifest{Format: FormatVersion, Site: "acme", Taken: time.Now().UTC()}
	}
	raw, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ManifestName, Size: int64(len(raw)), Mode: 0600}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(raw); err != nil {
		t.Fatal(err)
	}
	for _, c := range []func() error{tw.Close, gz.Close, enc.Close} {
		if err := c(); err != nil {
			t.Fatal(err)
		}
	}
	return sealed.Bytes()
}

func TestRestoreFiles_PutsTheSiteBack(t *testing.T) {
	key := testKey(t)
	sealed := handMade(t, key, map[string]string{
		"files/index.php":           "<?php echo 'restored';",
		"files/.env":                "APP_KEY=secret",
		"files/app/Models/User.php": "<?php class User {}",
	}, nil)

	into := t.TempDir()
	man, err := RestoreFiles(bytes.NewReader(sealed), key, into)
	if err != nil {
		t.Fatal(err)
	}
	if man.Site != "acme" {
		t.Errorf("manifest says %q", man.Site)
	}
	for path, want := range map[string]string{
		"index.php":           "<?php echo 'restored';",
		".env":                "APP_KEY=secret",
		"app/Models/User.php": "<?php class User {}",
	} {
		got, err := os.ReadFile(filepath.Join(into, path))
		if err != nil {
			t.Errorf("%s was not restored: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s came back as %q", path, got)
		}
	}
}

// The one that matters. An archive is a file from somewhere else, and a tar
// entry naming ../ writes outside the directory it was told to write into.
// Restoring one would let an archive overwrite another site, or a unit file.
func TestRestoreFiles_RefusesAnEntryThatEscapesTheDirectory(t *testing.T) {
	key := testKey(t)
	for _, escape := range []string{
		"files/../../../etc/passwd",
		"files/../outside.txt",
		"/etc/passwd",
		"files/a/../../../../outside.txt",
		// The sibling escape: this lands in a directory whose path merely
		// starts with the site's, which a prefix check without the separator
		// waves through. It is a different site's directory.
		"files/../site-evil/pwned.txt",
	} {
		sealed := handMade(t, key, map[string]string{escape: "owned"}, nil)
		into := filepath.Join(t.TempDir(), "site")
		if err := os.MkdirAll(into, 0755); err != nil {
			t.Fatal(err)
		}
		if _, err := RestoreFiles(bytes.NewReader(sealed), key, into); err == nil {
			t.Errorf("%q was restored rather than refused", escape)
		}
		// And nothing landed outside either way.
		for _, stray := range []string{"outside.txt", filepath.Join("site-evil", "pwned.txt")} {
			if _, err := os.Stat(filepath.Join(filepath.Dir(into), stray)); err == nil {
				t.Errorf("%q wrote %s outside the site", escape, stray)
			}
		}
	}
}

// A symlink in an archive is a way to write through it on the next entry, so
// links are not restored at all rather than being checked.
func TestRestoreFiles_DoesNotRestoreALink(t *testing.T) {
	key := testKey(t)
	var sealed bytes.Buffer
	enc, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "files/escape", Linkname: "/etc"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(Manifest{Format: FormatVersion, Site: "acme"})
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ManifestName, Size: int64(len(raw)), Mode: 0600})
	_, _ = tw.Write(raw)
	for _, c := range []func() error{tw.Close, gz.Close, enc.Close} {
		if err := c(); err != nil {
			t.Fatal(err)
		}
	}

	into := t.TempDir()
	if _, err := RestoreFiles(bytes.NewReader(sealed.Bytes()), key, into); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(into, "escape")); err == nil {
		t.Error("a symlink from the archive was restored")
	}
}

// An archive this servlo cannot read must be refused before anything is
// written, not halfway through.
func TestRestoreFiles_RefusesAnArchiveFromANewerServlo(t *testing.T) {
	key := testKey(t)
	sealed := handMade(t, key, map[string]string{"files/index.php": "x"},
		&Manifest{Format: FormatVersion + 1, Site: "acme"})

	into := t.TempDir()
	if _, err := RestoreFiles(bytes.NewReader(sealed), key, into); err == nil {
		t.Fatal("an archive from a newer servlo was restored")
	} else if !strings.Contains(err.Error(), "update servlo") {
		t.Errorf("error = %q, does not say what to do", err)
	}
}

// The dump is handed to the caller as a stream rather than written to disk, so
// a restore can pipe a multi-gigabyte dump into the engine without spooling it.
func TestOpenDump_StreamsTheDumpOut(t *testing.T) {
	key := testKey(t)
	sealed := handMade(t, key, map[string]string{
		"files/index.php": "x",
		DatabaseName:      "CREATE TABLE users (id int);",
	}, &Manifest{Format: FormatVersion, Site: "acme", Database: true, DatabaseName: "acme"})

	var got bytes.Buffer
	man, err := OpenDump(bytes.NewReader(sealed), key, &got)
	if err != nil {
		t.Fatal(err)
	}
	if !man.Database {
		t.Error("the manifest does not record a database")
	}
	if got.String() != "CREATE TABLE users (id int);" {
		t.Errorf("dump came back as %q", got.String())
	}
}

// An archive with no dump says so rather than handing back an empty one, which
// a restore would happily load over a working database.
func TestOpenDump_SaysWhenThereIsNoDump(t *testing.T) {
	key := testKey(t)
	sealed := handMade(t, key, map[string]string{"files/index.php": "x"}, nil)

	var got bytes.Buffer
	if _, err := OpenDump(bytes.NewReader(sealed), key, &got); err == nil {
		t.Fatal("an archive with no dump reported one")
	}
	if got.Len() != 0 {
		t.Error("something was written for an archive with no dump")
	}
}

var _ = io.Discard
