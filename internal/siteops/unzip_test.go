package siteops

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// zipOf builds an archive in memory from name/content pairs. A name ending in
// "/" is a directory entry.
func zipOf(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, body := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func extract(t *testing.T, dir string, archive []byte, opts ...func(*UnzipOptions)) (UnzipResult, error) {
	t.Helper()
	o := UnzipOptions{MaxBytes: 1 << 20, MaxFiles: 100}
	for _, f := range opts {
		f(&o)
	}
	return Unzip(bytes.NewReader(archive), int64(len(archive)), dir, o)
}

func TestUnzip_ExtractsAnOrdinaryArchive(t *testing.T) {
	dir := t.TempDir()
	res, err := extract(t, dir, zipOf(t, map[string]string{
		"index.php":        "<?php echo 1;",
		"app/Kernel.php":   "<?php",
		"public/index.php": "<?php",
	}))
	if err != nil {
		t.Fatalf("Unzip: %v", err)
	}
	if res.Files != 3 {
		t.Errorf("Files = %d, want 3", res.Files)
	}
	for _, want := range []string{"index.php", "app/Kernel.php", "public/index.php"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("%s missing: %v", want, err)
		}
	}
}

// Zip slip. An entry named ../../etc/cron.d/x writes outside the directory the
// operator chose, which on this machine means writing anywhere servlo can.
func TestUnzip_RefusesAnEntryThatEscapesTheDirectory(t *testing.T) {
	for _, name := range []string{
		"../escaped.php",
		"../../etc/passwd",
		"a/../../escaped.php",
		"/etc/passwd",
		`..\escaped.php`,
	} {
		dir := t.TempDir()
		outside := filepath.Dir(dir)

		_, err := extract(t, dir, zipOf(t, map[string]string{name: "pwned"}))
		if err == nil {
			t.Errorf("entry %q was accepted", name)
		}
		if matches, _ := filepath.Glob(filepath.Join(outside, "escaped.php")); len(matches) > 0 {
			t.Errorf("entry %q wrote outside the directory", name)
		}
	}
}

// A refused archive must not leave half of itself behind: the caller is about
// to register this directory as a site.
func TestUnzip_LeavesNothingBehindWhenItRefuses(t *testing.T) {
	dir := t.TempDir()

	_, err := extract(t, dir, zipOf(t, map[string]string{
		"good.php":       "<?php",
		"../escaped.php": "pwned",
	}))
	if err == nil {
		t.Fatal("the archive was accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("the directory holds %d entries after a refusal", len(entries))
	}
}

// A zip bomb is a small archive that expands to fill the disk, which takes
// every site on the machine down with it.
func TestUnzip_StopsAtTheSizeLimit(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("A", 4096)

	// An honest archive: the declared total is checked before a byte is
	// extracted, so this never reaches the write path at all.
	_, err := extract(t, dir, zipOf(t, map[string]string{"big.txt": big}), func(o *UnzipOptions) {
		o.MaxBytes = 1024
	})
	if err == nil {
		t.Fatal("an archive over the size limit was extracted")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, does not say the archive is too large", err)
	}
}

// Several entries, none of them over the limit on its own, that together are.
// The total is checked from the declared sizes before anything is written, so
// an oversized archive costs no disk at all rather than being noticed halfway
// through unpacking it.
func TestUnzip_StopsWhenTheEntriesOnlyExceedTheLimitTogether(t *testing.T) {
	dir := t.TempDir()
	entries := map[string]string{}
	for i := 0; i < 4; i++ {
		entries[string(rune('a'+i))+".txt"] = strings.Repeat("A", 600)
	}

	_, err := extract(t, dir, zipOf(t, entries), func(o *UnzipOptions) { o.MaxBytes = 1024 })
	if err == nil {
		t.Fatal("four 600-byte files were extracted under a 1024-byte limit")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, does not say the archive is too large", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("the refused archive left %d entries behind", len(left))
	}
}

func TestUnzip_StopsAtTheFileLimit(t *testing.T) {
	dir := t.TempDir()
	entries := map[string]string{}
	for i := 0; i < 20; i++ {
		entries[string(rune('a'+i))+".txt"] = "x"
	}

	_, err := extract(t, dir, zipOf(t, entries), func(o *UnzipOptions) { o.MaxFiles = 5 })
	if err == nil {
		t.Fatal("an archive over the file limit was extracted")
	}
}

// Everything a browser produces from "download ZIP" is wrapped in one
// directory named for the repository or the release. Extracting that verbatim
// gives a site whose document root is one level below where anyone expects.
func TestUnzip_UnwrapsASingleTopLevelDirectory(t *testing.T) {
	dir := t.TempDir()

	res, err := extract(t, dir, zipOf(t, map[string]string{
		"myapp-main/index.php":        "<?php",
		"myapp-main/public/index.php": "<?php",
	}))
	if err != nil {
		t.Fatalf("Unzip: %v", err)
	}
	if !res.Unwrapped {
		t.Error("Unwrapped = false, want true")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.php")); err != nil {
		t.Errorf("the wrapper directory was not unwrapped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "myapp-main")); err == nil {
		t.Error("the wrapper directory is still there")
	}
}

// Two top-level entries is a project that was zipped from inside its own
// directory, and moving either would be inventing a structure.
func TestUnzip_LeavesSeveralTopLevelEntriesAlone(t *testing.T) {
	dir := t.TempDir()

	res, err := extract(t, dir, zipOf(t, map[string]string{
		"index.php":        "<?php",
		"public/index.php": "<?php",
	}))
	if err != nil {
		t.Fatalf("Unzip: %v", err)
	}
	if res.Unwrapped {
		t.Error("Unwrapped = true for an archive with two top-level entries")
	}
	if _, err := os.Stat(filepath.Join(dir, "index.php")); err != nil {
		t.Errorf("index.php missing: %v", err)
	}
}

// zipWithMode builds a one-entry archive that really carries mode, which
// w.Create does not: it writes the writer's default and the header is ignored.
// The first version of this test used Create and so asserted nothing.
func zipWithMode(t *testing.T, name string, mode os.FileMode, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	f, err := w.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Extraction runs as the same user every site runs as, so a mode carried in
// from an archive is a mode somebody else chose for a file on this machine.
func TestUnzip_DoesNotHonourExecutableOrSetuidModes(t *testing.T) {
	dir := t.TempDir()

	if _, err := extract(t, dir, zipWithMode(t, "run.sh", os.ModeSetuid|0o777, "#!/bin/sh\n")); err != nil {
		t.Fatalf("Unzip: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSetuid != 0 {
		t.Error("a setuid bit survived extraction")
	}
	if info.Mode().Perm()&0o111 != 0 {
		t.Errorf("mode = %o, want no execute bits", info.Mode().Perm())
	}
}

// A symlink is a second way out of the site directory, and pointing it at
// /etc/passwd turns a file the site serves into a file the machine owns.
func TestUnzip_RefusesASymlink(t *testing.T) {
	dir := t.TempDir()

	_, err := extract(t, dir, zipWithMode(t, "sneaky", os.ModeSymlink|0o777, "/etc/passwd"))
	if err == nil {
		t.Fatal("an archive containing a symlink was extracted")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error = %q, does not say a symlink was the problem", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("the refused archive left something behind")
	}
}

// lyingZip declares a tiny uncompressed size for an entry that is anything but.
// This is the shape of a real zip bomb: the header claims one thing and the
// stream delivers another, so the check on the declared size is not enough on
// its own.
func lyingZip(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	raw := zipOf(t, map[string]string{name: string(body)})

	// The reader takes its sizes from the central directory, where the
	// uncompressed size is a uint32 at offset 24 of each record.
	const centralDirectorySignature = "PK\x01\x02"
	at := bytes.Index(raw, []byte(centralDirectorySignature))
	if at < 0 {
		t.Fatal("no central directory in the generated archive")
	}
	patched := append([]byte(nil), raw...)
	for i, b := range []byte{1, 0, 0, 0} {
		patched[at+24+i] = b
	}
	return patched
}

// An archive whose header claims one size while its stream delivers another is
// the classic bomb. It is refused, and the directory is left as it was found.
//
// Not asserted: which check refuses it. archive/zip validates the stream
// against the declared size when the entry is opened, so the lie is caught
// before servlo's own byte count sees a single byte of it. That is a fine
// place for it to be caught, and pinning the message would pin somebody else's
// implementation detail.
func TestUnzip_RefusesAnArchiveThatLiesAboutItsSize(t *testing.T) {
	dir := t.TempDir()

	_, err := extract(t, dir, lyingZip(t, "big.txt", bytes.Repeat([]byte("A"), 8192)), func(o *UnzipOptions) {
		o.MaxBytes = 1024
	})
	if err == nil {
		t.Fatal("an archive that lied about its size was extracted")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the refused archive left %d entries behind", len(entries))
	}
}

// A directory that is not empty is somebody's site.
func TestUnzip_RefusesANonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := extract(t, dir, zipOf(t, map[string]string{"other.php": "<?php"})); err == nil {
		t.Fatal("extracting over an existing project was accepted")
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "index.php")); string(body) != "<?php" {
		t.Error("the existing file was overwritten")
	}
}

func TestUnzip_RefusesSomethingThatIsNotAZip(t *testing.T) {
	dir := t.TempDir()
	if _, err := extract(t, dir, []byte("this is not a zip file at all")); err == nil {
		t.Fatal("a non-archive was accepted")
	}
}
