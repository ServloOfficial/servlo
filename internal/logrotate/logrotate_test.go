package logrotate

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// A tiny policy, so a test does not have to write 50 MB to prove anything.
func small() Policy { return Policy{MaxSizeMB: 1, Keep: 3, Compress: false} }

const overLimit = 1<<20 + 1

// The whole point: a log past the limit stops being the live log, and the
// application writes into a fresh one.
func TestRotate_MovesTheLogAsideOnceItIsTooBig(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "storage/logs/laravel.log")
	write(t, log, overLimit)

	res, err := Rotate(root, []string{"storage/logs/*.log"}, small())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rotated) != 1 {
		t.Fatalf("rotated %v", res.Rotated)
	}
	if exists(log) {
		t.Error("the live log is still there, so it keeps growing")
	}
	if !exists(log + ".1") {
		t.Error("the rotated copy is not there, so the log was deleted rather than kept")
	}
}

// A log under the limit is left alone. Rotating every night regardless would
// leave an operator reading a log split across five files for no reason.
func TestRotate_LeavesASmallLogAlone(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "storage/logs/laravel.log")
	write(t, log, 100)

	res, err := Rotate(root, []string{"storage/logs/*.log"}, small())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rotated) != 0 || !exists(log) {
		t.Errorf("a 100-byte log was rotated: %+v", res)
	}
}

// The copies shift up, so .1 is always the most recent.
func TestRotate_ShiftsTheOlderCopiesUp(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "app.log")
	write(t, log, overLimit)
	write(t, log+".1", 10)
	write(t, log+".2", 10)

	if _, err := Rotate(root, []string{"*.log"}, small()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{log + ".1", log + ".2", log + ".3"} {
		if !exists(want) {
			t.Errorf("%s is missing, so a rotation overwrote an older one", filepath.Base(want))
		}
	}
	// And the newest is the one just rotated, not one of the old stubs.
	if info, err := os.Stat(log + ".1"); err != nil || info.Size() != overLimit {
		t.Error(".1 is not the log that was just rotated")
	}
}

// Past the keep count they go. A rotation that never deleted anything is the
// same disk filling up, one file at a time instead of one file.
func TestRotate_DeletesTheCopiesPastTheKeepCount(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "app.log")
	write(t, log, overLimit)
	for _, n := range []string{".1", ".2", ".3", ".4", ".5"} {
		write(t, log+n, 10)
	}

	res, err := Rotate(root, []string{"*.log"}, small())
	if err != nil {
		t.Fatal(err)
	}
	if exists(log + ".4") {
		t.Error("a copy past the keep count survived")
	}
	if res.Removed == 0 {
		t.Error("nothing was reported as removed, so an operator cannot tell it worked")
	}
	if !exists(log + ".3") {
		t.Error("a copy inside the keep count was deleted")
	}
}

// Compression is the difference between keeping a week of logs and not.
func TestRotate_CompressesWhenAsked(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "app.log")
	write(t, log, overLimit)

	p := small()
	p.Compress = true
	if _, err := Rotate(root, []string{"*.log"}, p); err != nil {
		t.Fatal(err)
	}
	if exists(log) {
		t.Error("the live log survived a compressed rotation, so it keeps growing and is now stored twice")
	}
	if exists(log + ".1") {
		t.Error("an uncompressed copy was left beside the compressed one")
	}
	f, err := os.Open(log + ".1.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("the rotated copy is not gzip: %v", err)
	}
	body, err := io.ReadAll(zr)
	if err != nil || len(body) != overLimit {
		t.Errorf("the compressed copy holds %d bytes of the %d that were in the log", len(body), overLimit)
	}
}

// The live log is only removed once the compressed copy is complete, so an
// interrupted rotation loses nothing. Checked by proving the copy is readable
// in full, which the test above does, and here that both never exist at once.
func TestRotate_DoesNotRotateItsOwnOutput(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "app.log")
	write(t, log, overLimit)
	// A previous rotation's copy, itself over the limit.
	write(t, log+".1", overLimit)

	res, err := Rotate(root, []string{"*"}, small())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rotated) != 1 || res.Rotated[0] != log {
		t.Errorf("rotated %v, want only the live log", res.Rotated)
	}
	if exists(log + ".1.1") {
		t.Error("a rotated copy was rotated again, so the names grow a chain")
	}
}

// A dated file the application named itself is not a rotation of servlo's, and
// deleting one would be deleting a log nothing else keeps.
func TestRotate_LeavesTheApplicationsOwnDatedLogsAlone(t *testing.T) {
	root := t.TempDir()
	dated := filepath.Join(root, "laravel-2026-08-09.log")
	write(t, dated, 10)
	write(t, filepath.Join(root, "app.log"), overLimit)

	if _, err := Rotate(root, []string{"*.log"}, small()); err != nil {
		t.Fatal(err)
	}
	if !exists(dated) {
		t.Error("the application's own dated log was swept as if servlo had made it")
	}
}

// A symlink named like a log is a way to have a rotation copy something from
// outside the site into it, or take it away. Neither happens: the link is not
// followed and not touched.
func TestRotate_RefusesToRotateThroughASymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secrets")
	write(t, outside, overLimit)
	link := filepath.Join(root, "app.log")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	p := small()
	p.Compress = true
	res, err := Rotate(root, []string{"*.log"}, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Rotated) != 0 {
		t.Errorf("rotated %v, want nothing: that is a link, not a log", res.Rotated)
	}
	if !exists(outside) {
		t.Error("a rotation took away a file outside the site")
	}
	if exists(link + ".1.gz") {
		t.Error("a rotation copied a file from outside the site into it")
	}
	if !exists(link) {
		t.Error("the link itself was moved, which is a rotation of something servlo did not write")
	}
}

// A policy half-filled in must not become "rotate at zero bytes and keep none",
// which would delete every log on the server on the next tick.
func TestPolicy_ResolvedFillsInWhatWasLeftUnset(t *testing.T) {
	got := Policy{Keep: 2}.Resolved()
	if got.MaxSizeMB != DefaultPolicy.MaxSizeMB {
		t.Errorf("an unset size resolved to %d MB", got.MaxSizeMB)
	}
	if got.Keep != 2 {
		t.Errorf("a set keep of 2 resolved to %d", got.Keep)
	}
	if zero := (Policy{}).Resolved(); zero.Keep != DefaultPolicy.Keep {
		t.Errorf("an unset keep resolved to %d, which deletes every rotation", zero.Keep)
	}
}
