package auditlog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func auditTestDir(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// A log nothing ever removes fills the disk, and a log the application
// truncates is a log that tells you what the last writer wanted you to believe.
// Rotation is the only way to have both: the live file is only ever appended
// to, and it is renamed rather than emptied when it grows.
func TestRotate_RenamesRatherThanTruncates(t *testing.T) {
	auditTestDir(t)

	for i := 0; i < 40; i++ {
		if err := Append(Entry{Action: "test.event", Subject: strings.Repeat("x", 200)}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	before, err := os.ReadFile(Path())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}

	if err := rotateIfLarger(1024); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	// The live file is gone or empty, and everything that was in it is in the
	// rotated one. Nothing is lost, which is the difference between rotating
	// and truncating.
	rotated := rotatedFiles(t)
	if len(rotated) != 1 {
		t.Fatalf("found %d rotated files, want 1", len(rotated))
	}
	kept, err := os.ReadFile(rotated[0])
	if err != nil {
		t.Fatalf("reading the rotated file: %v", err)
	}
	if string(kept) != string(before) {
		t.Error("the rotated file does not hold what the live one did")
	}
}

// Appending after a rotation keeps working, and starts a fresh live file.
func TestRotate_AppendingAfterwardsStartsAFreshFile(t *testing.T) {
	auditTestDir(t)

	if err := Append(Entry{Action: "before.rotation"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := rotateIfLarger(0); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := Append(Entry{Action: "after.rotation"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	// Read the live file directly: Recent deliberately spans rotations, and
	// what this test is about is that appending started a new file rather than
	// reopening the old one.
	live, err := os.ReadFile(Path())
	if err != nil {
		t.Fatalf("reading the live log: %v", err)
	}
	if strings.Contains(string(live), "before.rotation") {
		t.Error("the live log still holds what was rotated away")
	}
	if !strings.Contains(string(live), "after.rotation") {
		t.Error("the live log does not hold what was written after the rotation")
	}
	// The rotated file keeps its permissions: it is the same secrets in a file
	// with a different name.
	info, err := os.Stat(rotatedFiles(t)[0])
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("rotated file mode = %04o, want 0600", mode)
	}
}

// A log below the threshold is left alone, or every append would rename the
// file and the log would be one entry long forever.
func TestRotate_LeavesASmallLogAlone(t *testing.T) {
	auditTestDir(t)

	if err := Append(Entry{Action: "small"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := rotateIfLarger(1 << 20); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if files := rotatedFiles(t); len(files) != 0 {
		t.Errorf("a small log was rotated: %v", files)
	}
}

// Rotating a log that does not exist is not an error: it is the ordinary state
// of a machine where nothing has happened yet.
func TestRotate_AbsentLogIsFine(t *testing.T) {
	auditTestDir(t)
	if err := rotateIfLarger(1024); err != nil {
		t.Errorf("rotating an absent log: %v", err)
	}
}

// Old rotations are pruned, or rotation trades one unbounded file for an
// unbounded number of them.
func TestRotate_PrunesTheOldest(t *testing.T) {
	auditTestDir(t)

	for i := 0; i < keptRotations+3; i++ {
		if err := Append(Entry{Action: "event"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := rotateIfLarger(0); err != nil {
			t.Fatalf("rotate: %v", err)
		}
	}
	if files := rotatedFiles(t); len(files) > keptRotations {
		t.Errorf("kept %d rotations, want at most %d", len(files), keptRotations)
	}
}

// Recent reads across a rotation, or the dashboard's history vanishes the
// moment the log grows.
func TestRecent_ReadsAcrossARotation(t *testing.T) {
	auditTestDir(t)

	if err := Append(Entry{Action: "older.event"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := rotateIfLarger(0); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if err := Append(Entry{Action: "newer.event"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	entries, err := Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Recent returned %d entries, want 2 across the rotation: %+v", len(entries), entries)
	}
	if entries[0].Action != "newer.event" || entries[1].Action != "older.event" {
		t.Errorf("entries out of order: %+v", entries)
	}
}

func rotatedFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob(Path() + ".*")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sort.Strings(matches)
	return matches
}
