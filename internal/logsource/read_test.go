package logsource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const monologFixture = `[2026-06-11 10:00:00] local.INFO: started up
[2026-06-11 10:05:00] local.ERROR: SQLSTATE[HY000] connection refused
[2026-06-11 10:10:00] local.WARNING: slow query detected
[2026-06-11 10:15:00] local.ERROR: SQLSTATE[42S02] table missing
`

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "laravel.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func monologSource(t *testing.T, content string) Source {
	return Source{Name: "app:laravel.log", Kind: KindFile, Locator: writeFixture(t, content), Format: "monolog"}
}

func TestRead_File_NoFilter_Chronological(t *testing.T) {
	res, err := Read(monologSource(t, monologFixture), Opts{Lines: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 4 {
		t.Fatalf("want 4 entries, got %d", len(res.Entries))
	}
	if !contains(res.Entries[0].Text, "started up") {
		t.Errorf("first entry should be oldest, got %q", res.Entries[0].Text)
	}
	if !contains(res.Entries[3].Text, "table missing") {
		t.Errorf("last entry should be newest, got %q", res.Entries[3].Text)
	}
	if res.Cursor != "2026-06-11 10:15:00" {
		t.Errorf("cursor = %q, want newest entry date", res.Cursor)
	}
}

func TestRead_File_Grep(t *testing.T) {
	cases := []struct {
		name string
		grep string
		want int
	}{
		{"literal-substring-as-regex", "SQLSTATE", 2},
		{"regex", "42S0[0-9]", 1},
		{"invalid-regex-falls-back-to-literal", "[", 2},
		{"no-match", "nope", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Read(monologSource(t, monologFixture), Opts{Lines: 10, Grep: tc.grep})
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(res.Entries) != tc.want {
				t.Fatalf("grep %q: want %d entries, got %d", tc.grep, tc.want, len(res.Entries))
			}
		})
	}
}

func TestRead_File_Level(t *testing.T) {
	res, err := Read(monologSource(t, monologFixture), Opts{Lines: 10, Level: "error"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("want 2 error entries, got %d", len(res.Entries))
	}
	for _, e := range res.Entries {
		if e.Level != "ERROR" {
			t.Errorf("entry level = %q, want ERROR", e.Level)
		}
	}
}

func TestRead_File_LinesCap(t *testing.T) {
	res, err := Read(monologSource(t, monologFixture), Opts{Lines: 2})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(res.Entries))
	}
	// Newest two, still chronological.
	if !contains(res.Entries[0].Text, "slow query") || !contains(res.Entries[1].Text, "table missing") {
		t.Errorf("unexpected window: %q / %q", res.Entries[0].Text, res.Entries[1].Text)
	}
}

func TestRead_File_TimeWindow(t *testing.T) {
	res, err := Read(monologSource(t, monologFixture), Opts{Lines: 10, Since: "2026-06-11 10:07:00"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 2 {
		t.Fatalf("want 2 entries after 10:07, got %d", len(res.Entries))
	}
	if !contains(res.Entries[0].Text, "slow query") {
		t.Errorf("first kept entry = %q", res.Entries[0].Text)
	}
}

func TestRead_File_CursorAdvances(t *testing.T) {
	src := monologSource(t, monologFixture)
	first, err := Read(src, Opts{Lines: 10})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	second, err := Read(src, Opts{Lines: 10, Since: first.Cursor})
	if err != nil {
		t.Fatalf("Read poll: %v", err)
	}
	// Only the boundary (newest) entry remains, not the whole file again.
	if len(second.Entries) != 1 || !contains(second.Entries[0].Text, "table missing") {
		t.Fatalf("polling with cursor should narrow to the newest entry, got %d: %+v", len(second.Entries), second.Entries)
	}
}

func TestRead_RawFile_SinceIsBestEffortNoOp(t *testing.T) {
	raw := "line one\nline two\nline three\n"
	src := Source{Name: "raw", Kind: KindFile, Locator: writeFixture(t, raw), Format: "raw"}
	res, err := Read(src, Opts{Lines: 10, Since: "5m"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("raw since should be a no-op (last-N), got %d entries", len(res.Entries))
	}
	if res.Cursor != "" {
		t.Errorf("raw source has no timestamp cursor, got %q", res.Cursor)
	}
}

func TestParseSince(t *testing.T) {
	now := time.Now()
	cases := []struct {
		in   string
		ok   bool
		near time.Time
	}{
		{"15m", true, now.Add(-15 * time.Minute)},
		{"2h30m", true, now.Add(-150 * time.Minute)},
		{"2026-06-11T10:00:00Z", true, time.Date(2026, 6, 11, 10, 0, 0, 0, time.UTC)},
		{"", false, time.Time{}},
		{"garbage", false, time.Time{}},
	}
	for _, tc := range cases {
		got, ok := parseSince(tc.in)
		if ok != tc.ok {
			t.Errorf("parseSince(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if tc.ok && got.Sub(tc.near).Abs() > 2*time.Second {
			t.Errorf("parseSince(%q) = %v, want near %v", tc.in, got, tc.near)
		}
	}
}

func TestCompileGrep(t *testing.T) {
	if compileGrep("") != nil {
		t.Error("empty pattern should yield no matcher")
	}
	re := compileGrep("ab.c")
	if !re("abXc") || re("abc") {
		t.Error("regex matcher behaved unexpectedly")
	}
	lit := compileGrep("[") // invalid regex
	if !lit("a[b") || lit("ab") {
		t.Error("literal fallback behaved unexpectedly")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
