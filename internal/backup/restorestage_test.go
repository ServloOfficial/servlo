package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// ordered builds an archive with the entries in the order given, which the map
// handMade takes cannot express. Every refusal below needs a good entry to come
// before the bad one: that is the whole question, whether the good one is on
// disk by the time the bad one is refused.
type archiveEntry struct{ name, body string }

func ordered(t *testing.T, key []byte, entries []archiveEntry, man *Manifest) []byte {
	t.Helper()
	var sealed bytes.Buffer
	enc, err := NewEncryptor(&sealed, key)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: e.name, Size: int64(len(e.body)), Mode: 0644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if man != nil {
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
	}
	for _, c := range []func() error{tw.Close, gz.Close, enc.Close} {
		if err := c(); err != nil {
			t.Fatal(err)
		}
	}
	return sealed.Bytes()
}

// snapshot describes a directory tree by name, kind and content, so a test can
// say a refusal changed nothing rather than checking the few paths it thought
// of.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			lines = append(lines, "dir  "+rel)
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		lines = append(lines, "file "+rel+" "+hex.EncodeToString(sum[:8]))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// seedSite lays down a site that is already being served, which is what a
// restore is aimed at.
func seedSite(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "storage"), 0755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		"index.php":        "<?php // the site as it stands\n",
		"storage/logs.txt": "yesterday\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

// A restore that is refused must leave the site alone.
//
// Every one of these archives holds a perfectly ordinary file before the entry
// that earns the refusal, and the manifest servlo writes comes last, so an
// unpack straight into the site directory has already written that file by the
// time it knows the archive is not one it will accept. The operator reads that
// nothing was restored; the site disagrees.
func TestRestoreFiles_ARefusedArchiveLeavesTheSiteAsItWas(t *testing.T) {
	good := archiveEntry{"files/index.php", "<?php // from the archive\n"}
	fresh := &Manifest{Format: FormatVersion, Site: "acme", Taken: time.Now().UTC()}

	for _, tc := range []struct {
		why     string
		entries []archiveEntry
		man     *Manifest
	}{
		{"an entry that escapes the site", []archiveEntry{good, {"files/../outside.txt", "owned"}}, fresh},
		{"an entry servlo does not recognise", []archiveEntry{good, {"junk/x", "owned"}}, fresh},
		{"an archive of the server's state", []archiveEntry{good, {"config/settings.yaml", "owned"}}, fresh},
		{"an archive from a newer servlo", []archiveEntry{good}, &Manifest{Format: FormatVersion + 1, Site: "acme"}},
		{"an archive that was interrupted", []archiveEntry{good}, nil},
	} {
		t.Run(tc.why, func(t *testing.T) {
			k := testKey(t)
			parent := t.TempDir()
			root := filepath.Join(parent, "acme")
			seedSite(t, root)
			before := snapshot(t, root)

			sealed := ordered(t, k, tc.entries, tc.man)
			if _, err := RestoreFiles(bytes.NewReader(sealed), k, root); err == nil {
				t.Fatal("the archive was restored rather than refused")
			}

			if after := snapshot(t, root); after != before {
				t.Errorf("a refused archive changed the site:\nbefore:\n%s\nafter:\n%s", before, after)
			}
			leftovers(t, parent, "acme")
		})
	}
}

// The same for the server's own state, where half of what is being written back
// is a credential.
func TestRestoreState_ARefusedArchiveLeavesTheServerAsItWas(t *testing.T) {
	k := testKey(t)
	parent := t.TempDir()
	configDir := filepath.Join(parent, "config")
	dataDir := filepath.Join(parent, "data")
	for _, dir := range []string{configDir, dataDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.yaml"), []byte("as it stands\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot(t, configDir) + "\n" + snapshot(t, dataDir)

	sealed := ordered(t, k, []archiveEntry{
		{"config/settings.yaml", "from the archive\n"},
		{"files/index.php", "owned"},
	}, &Manifest{Format: FormatVersion, Kind: KindState, Taken: time.Now().UTC()})
	if _, err := RestoreState(bytes.NewReader(sealed), k, configDir, dataDir); err == nil {
		t.Fatal("a site archive was unpacked over the server's configuration")
	}

	if after := snapshot(t, configDir) + "\n" + snapshot(t, dataDir); after != before {
		t.Errorf("a refused archive changed the server:\nbefore:\n%s\nafter:\n%s", before, after)
	}
	leftovers(t, parent, "config", "data")
}

// An accepted archive still restores, and still restores the way it always did:
// what the archive holds goes back, what it does not hold is left alone.
func TestRestoreFiles_PutsBackWhatTheArchiveHoldsAndLeavesTheRestAlone(t *testing.T) {
	k := testKey(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "acme")
	seedSite(t, root)

	sealed := ordered(t, k, []archiveEntry{
		{"files/index.php", "<?php // from the archive\n"},
		{"files/storage/keep/nested.txt", "nested\n"},
	}, &Manifest{Format: FormatVersion, Site: "acme", Taken: time.Now().UTC()})
	if _, err := RestoreFiles(bytes.NewReader(sealed), k, root); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		"index.php":               "<?php // from the archive\n",
		"storage/keep/nested.txt": "nested\n",
		"storage/logs.txt":        "yesterday\n",
	} {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	leftovers(t, parent, "acme")
}

// A file in the archive standing where the site has a directory, and the
// reverse. Rename replaces a file with a file on its own and refuses the rest,
// so both of these are a restore that half happens if nothing clears the way.
func TestRestoreFiles_ReplacesAPathThatChangedKind(t *testing.T) {
	k := testKey(t)
	parent := t.TempDir()
	root := filepath.Join(parent, "acme")
	if err := os.MkdirAll(filepath.Join(root, "index.php", "surprise"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "storage"), []byte("a file where a directory goes\n"), 0644); err != nil {
		t.Fatal(err)
	}

	sealed := ordered(t, k, []archiveEntry{
		{"files/index.php", "<?php\n"},
		{"files/storage/logs.txt", "logs\n"},
	}, &Manifest{Format: FormatVersion, Site: "acme", Taken: time.Now().UTC()})
	if _, err := RestoreFiles(bytes.NewReader(sealed), k, root); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]string{
		"index.php":        "<?php\n",
		"storage/logs.txt": "logs\n",
	} {
		got, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

// leftovers fails if the restore left its scratch directory behind. It sits
// beside the site, so one abandoned per failed restore would accumulate in the
// directory sites are listed from.
func leftovers(t *testing.T, parent string, expected ...string) {
	t.Helper()
	keep := map[string]bool{}
	for _, name := range expected {
		keep[name] = true
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			t.Errorf("the restore left %s behind next to the site", e.Name())
		}
	}
}
