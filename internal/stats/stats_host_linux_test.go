//go:build linux

package stats

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCgroupStatKey(t *testing.T) {
	dir := t.TempDir()
	stat := filepath.Join(dir, "memory.stat")
	body := "anon 24117248\nfile 664797184\ninactive_file 600000000\nkernel 38797312\n"
	if err := os.WriteFile(stat, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readCgroupStatKey(stat, "inactive_file"); got != 600000000 {
		t.Errorf("inactive_file = %d, want 600000000", got)
	}
	if got := readCgroupStatKey(stat, "anon"); got != 24117248 {
		t.Errorf("anon = %d, want 24117248", got)
	}
	if got := readCgroupStatKey(stat, "missing"); got != 0 {
		t.Errorf("missing key = %d, want 0", got)
	}
	if got := readCgroupStatKey(filepath.Join(dir, "nope"), "anon"); got != 0 {
		t.Errorf("unreadable file = %d, want 0", got)
	}
}

func TestCgroupWorkingSet(t *testing.T) {
	// cgroupWorkingSet prefixes /sys/fs/cgroup, so point it at a fake tree there
	// is not possible without root; instead exercise the subtraction via the
	// helpers it composes, which is where the logic lives.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "memory.current"), []byte("730000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "memory.stat"), []byte("inactive_file 600000000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur, err := readCgroupInt(filepath.Join(dir, "memory.current"))
	if err != nil {
		t.Fatal(err)
	}
	ws := cur - readCgroupStatKey(filepath.Join(dir, "memory.stat"), "inactive_file")
	if ws != 130000000 {
		t.Errorf("working set = %d, want 130000000 (current - inactive_file)", ws)
	}

	// Empty cgroup path is the no-data signal: fall back to MemoryCurrent.
	if _, ok := cgroupWorkingSet(""); ok {
		t.Errorf("cgroupWorkingSet(\"\") should report ok=false")
	}
}
