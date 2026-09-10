package webhook

import (
	"os"
	"path/filepath"
	"testing"
)

// The delivery file is what tells a replay from a new push, and the package
// refuses a deploy outright when it cannot be read, which is the right way
// round: a signed body stays valid forever, so waving one through because the
// record of it went missing is the failure this whole file exists to stop.
//
// That refusal is what makes the write worth getting right. os.WriteFile empties
// a file before it writes a byte, so a disk that fills in between leaves a few
// bytes of JSON that will never parse again, and from then on every push to that
// site is refused. Not until the disk is freed: for good, because the unparseable
// file is still there and nothing rewrites it. Deploys stop, and the reason is a
// disk that filled up once.
//
// Replacing the file by rename costs the staging file instead.
func TestSeen_ReplacesTheDeliveryFileRatherThanRewritingIt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)

	if seen, err := Seen("shop", "delivery-one"); err != nil || seen {
		t.Fatalf("Seen(first) = %v, %v; want false, nil", seen, err)
	}
	path, err := deliveryPath("shop")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if seen, err := Seen("shop", "delivery-two"); err != nil || seen {
		t.Fatalf("Seen(second) = %v, %v; want false, nil", seen, err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the delivery file was rewritten in place, so a write that runs out of disk refuses every later push to this site")
	}

	// The replay is still recognised, and the first delivery was not lost.
	if seen, err := Seen("shop", "delivery-one"); err != nil || !seen {
		t.Errorf("Seen(replay) = %v, %v; want true, nil", seen, err)
	}
	if got := after.Mode().Perm(); got != 0o600 {
		t.Errorf("delivery file mode = %v, want 0600", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			t.Errorf("a staging file was left behind: %s", e.Name())
		}
	}
}
